import { resolve } from "node:path";
import os from "node:os";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const projectRoot = fileURLToPath(new URL(".", import.meta.url));

export default defineConfig({
  resolve: {
    alias: {
      "@": resolve(projectRoot, "src"),
    },
  },
  test: {
    include: ["tests/**/*.test.ts"],
    exclude: ["e2e/**", "node_modules/**"],
    setupFiles: ["./tests/setup-runtime-config.ts"],
    env: {
      XDG_CONFIG_HOME: resolve(os.tmpdir(), "review-hub-vitest", "config"),
    },
  },
});
