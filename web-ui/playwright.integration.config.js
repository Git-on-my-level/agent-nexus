import { defineConfig } from "@playwright/test";

import { isCi, resolvePort, reuseExistingServer } from "./playwright.ports.js";

// Core runs outside this config (ANX_CORE_BASE_URL), so only the UI port is ours.
const port = resolvePort("webUi", "PLAYWRIGHT_PORT");

export default defineConfig({
  testDir: "tests/e2e",
  testMatch: /integration-core-golden-path\.spec\.js/,
  fullyParallel: false,
  retries: isCi ? 1 : 0,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    headless: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  outputDir: "test-results/e2e-with-core",
  webServer: {
    command: `pnpm exec vite dev --host 127.0.0.1 --port ${port} --strictPort`,
    port,
    timeout: 120000,
    reuseExistingServer: reuseExistingServer(
      "PLAYWRIGHT_REUSE_EXISTING_WEB_UI",
    ),
  },
});
