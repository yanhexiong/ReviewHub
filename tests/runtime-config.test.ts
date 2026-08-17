import {
  mkdir,
  mkdtemp,
  readFile,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  loadRuntimeConfig,
  runtimeConfigPath,
} from "../src/lib/runtime-config";

describe("运行配置", () => {
  it("忽略应用环境变量，并创建受保护的非敏感启动配置", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "review-hub-runtime-"));
    const configHome = path.join(root, "config");
    const options = {
      appRoot: root,
      environment: {
        XDG_CONFIG_HOME: configHome,
        PAPER_REVIEW_DATA_DIR: path.join(root, "ignored-data"),
        PAPER_REVIEW_ALLOWED_ROOTS: path.join(root, "ignored-root"),
        SESSION_SECRET: "user-supplied-secret-must-be-ignored",
      },
      homeDirectory: root,
    };
    const configFile = runtimeConfigPath(options);

    try {
      const configuration = loadRuntimeConfig(options);
      expect(configuration.dataDirectory).toBe(path.join(root, "data"));
      expect(configuration.listenHost).toBe("0.0.0.0");
      expect(configuration.listenPort).toBe(3000);
      expect(configuration.defaults.allowedRoots).toEqual([root]);
      expect(configuration.defaults.maxPdfBytes).toBe(104_857_600);

      const saved = JSON.parse(await readFile(configFile, "utf8")) as Record<
        string,
        unknown
      >;
      expect(saved).not.toHaveProperty("sessionSecret");
      expect(saved).not.toHaveProperty("encryptionKey");
      expect((await stat(path.dirname(configFile))).mode & 0o777).toBe(0o700);
      expect((await stat(configFile)).mode & 0o777).toBe(0o600);
    } finally {
      await rm(root, { force: true, recursive: true });
    }
  });

  it("拒绝损坏的运行配置，而不是静默覆盖它", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "review-hub-runtime-"));
    const options = {
      appRoot: root,
      environment: { XDG_CONFIG_HOME: path.join(root, "config") },
      homeDirectory: root,
    };
    try {
      const configFile = runtimeConfigPath(options);
      await mkdir(path.dirname(configFile), { recursive: true, mode: 0o700 });
      await writeFile(configFile, "{broken", { encoding: "utf8", mode: 0o600 });
      await expect(
        Promise.resolve().then(() => loadRuntimeConfig(options)),
      ).rejects.toThrow("运行配置文件无效");
    } finally {
      await rm(root, { force: true, recursive: true });
    }
  });
});
