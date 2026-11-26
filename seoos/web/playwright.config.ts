import { defineConfig } from "@playwright/test";

const port = Number(process.env.SEOOS_E2E_WEB_PORT ?? "3199");

export default defineConfig({
  testDir: "./tests",
  timeout: 30000,
  use: {
    baseURL: "http://localhost:" + port,
    channel: process.env.SEOOS_E2E_BROWSER_CHANNEL || undefined,
  },
  webServer: {
    command: "npm run start -- --port " + port,
    url: "http://127.0.0.1:" + port,
    timeout: 30000,
    reuseExistingServer: false,
  },
});
