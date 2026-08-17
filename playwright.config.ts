import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  use: { baseURL: "http://127.0.0.1:3000" },
  expect: { timeout: 15_000 },
  webServer: {
    command: "bash scripts/start-e2e.sh",
    url: "http://127.0.0.1:3000/api/system/health",
    reuseExistingServer: true,
    timeout: 180_000,
  },
});
