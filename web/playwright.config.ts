import { defineConfig } from "@playwright/test";

const port = Number(process.env.SEOOS_E2E_WEB_PORT ?? "3199");
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./tests",
  timeout: 45000,
  use: { baseURL, channel: process.env.SEOOS_E2E_BROWSER_CHANNEL || undefined },
  webServer: {
    command: `npm run start -- --hostname 127.0.0.1 --port ${port}`,
    url: `${baseURL}/seo/login`,
    timeout: 30000,
    reuseExistingServer: false,
  },
});
