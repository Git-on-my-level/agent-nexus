import { defineConfig } from "@playwright/test";

const port = Number(process.env.PLAYWRIGHT_PORT ?? 4173);

export default defineConfig({
  testDir: "tests/e2e",
  testMatch: /integration-core-golden-path\.spec\.js/,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
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
    command: `pnpm exec vite dev --host 127.0.0.1 --port ${port}`,
    port,
    timeout: 120000,
    reuseExistingServer: !process.env.CI,
  },
});
