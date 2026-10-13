import { defineConfig } from "@playwright/test"

export default defineConfig({
  testDir: "e2e",
  timeout: 30_000,
  retries: 0,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: process.env.PANEL_URL ?? "http://127.0.0.1:58080",
    channel: process.env.PANEL_BROWSER_CHANNEL ?? "chrome",
    viewport: { width: 1280, height: 900 },
  },
})
