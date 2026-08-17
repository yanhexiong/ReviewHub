import { AsyncLocalStorage } from "node:async_hooks";
import { spawn, type ChildProcess } from "node:child_process";
import { createServer, type ServerResponse } from "node:http";
import { createRequire } from "node:module";
import type { Duplex } from "node:stream";
import httpProxy from "http-proxy";

// Next's CLI normally installs this global before rendering App Router routes.
// A custom server must provide the same Node runtime baseline explicitly.
const nodeGlobal = globalThis as typeof globalThis & {
  AsyncLocalStorage?: typeof AsyncLocalStorage;
};
if (typeof nodeGlobal.AsyncLocalStorage !== "function")
  nodeGlobal.AsyncLocalStorage = AsyncLocalStorage;

const dev = process.env.NODE_ENV !== "production";
// Do not load .env.local. Runtime secrets and configuration are Go-owned.
process.env.__NEXT_PROCESSED_ENV = "true";
const proxy = httpProxy.createProxyServer({ xfwd: true, ws: true });

function writeApiError(
  response: ServerResponse,
  status: number,
  code: string,
  message: string,
) {
  if (response.headersSent) {
    response.destroy();
    return;
  }
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
  });
  response.end(JSON.stringify({ error: { code, message } }));
}

function writeSocketError(socket: Duplex, status: number) {
  const reason = status === 502 ? "Bad Gateway" : "Service Unavailable";
  socket.write(`HTTP/1.1 ${status} ${reason}\r\nConnection: close\r\n\r\n`);
  socket.destroy();
}

function isGoRequest(urlValue: string | undefined) {
  const pathname = new URL(urlValue ?? "/", "http://review-hub").pathname;
  return (
    pathname === "/api" ||
    pathname.startsWith("/api/") ||
    pathname === "/vscode" ||
    pathname.startsWith("/vscode/")
  );
}

function waitForChildStart(child: ChildProcess) {
  return new Promise<void>((resolve, reject) => {
    child.once("spawn", resolve);
    child.once("error", reject);
  });
}

function disableNextDotenvLoader() {
  const require = createRequire(import.meta.url);
  const modulePath = require.resolve("@next/env");
  require(modulePath);
  const moduleRecord = require.cache[modulePath];
  if (!moduleRecord) throw new Error("Next environment loader is unavailable");
  moduleRecord.exports = {
    ...moduleRecord.exports,
    loadEnvConfig: () => ({
      combinedEnv: process.env,
      parsedEnv: {},
      loadedEnvFiles: [],
    }),
  };
}

async function waitForGoApi(url: string, child: ChildProcess | null) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    if (child && child.exitCode !== null)
      throw new Error("Go API process exited before it became ready");
    try {
      const response = await fetch(`${url}/api/system/health`, {
        signal: AbortSignal.timeout(1_000),
      });
      if (response.ok) return;
    } catch {
      // The bounded retry avoids exposing internal process details to clients.
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(
    "Go API did not pass the startup health check; start the configured Go API or set REVIEW_HUB_GO_API_BIN",
  );
}

async function main() {
  const { loadRuntimeConfig } = await import("./src/lib/runtime-config");
  const runtimeConfig = loadRuntimeConfig();
  disableNextDotenvLoader();
  const { default: next } = await import("next");

  // The frontend listener is persisted by Go during setup/admin changes and
  // read here on the next process start. The Go API keeps its own loopback
  // listener in runtimeConfig.goApiListen.
  const hostname = runtimeConfig.listenHost;
  const port = runtimeConfig.listenPort;
  const app = next({ dev, hostname, port, dir: process.cwd() });
  const handle = app.getRequestHandler();
  await app.prepare();
  const upgradeNext = app.getUpgradeHandler();

  const goApiAddress = runtimeConfig.goApiListen;
  const goApiTarget = `http://${goApiAddress}`;
  const goApiBinary = process.env.REVIEW_HUB_GO_API_BIN?.trim();
  let goApiProcess: ChildProcess | null = null;
  if (goApiBinary) {
    goApiProcess = spawn(goApiBinary, [], {
      env: { ...process.env },
      stdio: "inherit",
    });
  }
  try {
    if (goApiProcess) await waitForChildStart(goApiProcess);
    await waitForGoApi(goApiTarget, goApiProcess);
  } catch (error) {
    if (goApiProcess && goApiProcess.exitCode === null)
      goApiProcess.kill("SIGTERM");
    throw error;
  }

  const server = createServer((request, response) => {
    if (isGoRequest(request.url)) {
      proxy.web(
        request,
        response,
        { target: goApiTarget, changeOrigin: false },
        () =>
          writeApiError(
            response,
            502,
            "GO_API_UNAVAILABLE",
            "API 服务暂时不可用，请稍后重试",
          ),
      );
      return;
    }
    void handle(request, response);
  });
  server.on("upgrade", (request, socket, head) => {
    if (isGoRequest(request.url)) {
      proxy.ws(
        request,
        socket,
        head,
        { target: goApiTarget, changeOrigin: false },
        () => writeSocketError(socket, 502),
      );
      return;
    }
    void upgradeNext(request, socket, head);
  });

  const shutdown = async () => {
    server.close();
    if (goApiProcess && goApiProcess.exitCode === null)
      goApiProcess.kill("SIGTERM");
    await app.close();
  };
  process.once("SIGINT", () => void shutdown());
  process.once("SIGTERM", () => void shutdown());
  server.listen(port, hostname, () => {
    console.log(`Review Hub ready on http://${hostname}:${port}`);
  });
}

void main();
