const { defineConfig } = require("@playwright/test");

const baseURL = process.env.ANX_LIVE_UI_URL || "http://127.0.0.1:8301";

module.exports = defineConfig({
  testDir: ".",
  testMatch: /live_(web_pm|source_drag)\.spec\.js/,
  workers: 1,
  retries: 0,
  timeout: 12 * 60 * 1000,
  outputDir: ".cache/playwright-live",
  reporter: [["list"], ["json", { outputFile: ".cache/live-web-pm.local.json" }]],
  use: {
    baseURL,
    headless: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});
