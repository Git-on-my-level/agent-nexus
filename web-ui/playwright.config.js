import { defineConfig } from "@playwright/test";

const port = Number(process.env.PLAYWRIGHT_PORT ?? 4173);

export default defineConfig({
  testDir: "tests/e2e",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: "list",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    headless: true,
    trace: "on-first-retry",
  },
  webServer: {
    command: `pnpm exec vite dev --host 127.0.0.1 --port ${port}`,
    port,
    timeout: 120000,
    reuseExistingServer: !process.env.CI,
  },
});
