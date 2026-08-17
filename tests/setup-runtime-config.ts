import { mkdirSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";

const configHome =
  process.env.XDG_CONFIG_HOME ??
  path.join(os.tmpdir(), "review-hub-vitest", "config");
const dataDirectory = path.join(os.tmpdir(), "review-hub-vitest", "data");
const configDirectory = path.join(configHome, "review-hub");

mkdirSync(configDirectory, { recursive: true, mode: 0o700 });
writeFileSync(
  path.join(configDirectory, "runtime-config.json"),
  `${JSON.stringify({
    version: 1,
    dataDirectory,
    goApiListen: "127.0.0.1:39100",
    listenHost: "0.0.0.0",
    listenPort: 3000,
    defaults: {
      allowedRoots: [dataDirectory],
      maxPdfBytes: 104_857_600,
      maxImportBytes: 1_073_741_824,
      maxProjectsPerUser: 20,
      maxUsers: 1_000,
    },
  })}\n`,
  { mode: 0o600 },
);
