import { defineConfig } from "@playwright/test";

const port = Number(process.env.PLAYWRIGHT_PORT ?? 4173);
const basePathPort = Number(process.env.PLAYWRIGHT_BASE_PATH_PORT ?? 4174);
const appBasePath = process.env.PLAYWRIGHT_APP_BASE_PATH ?? "/oar";

export default defineConfig({
  testDir: "tests/e2e",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: "list",
  use: {
    headless: true,
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "default",
      testIgnore: /base-path\.spec\.js/,
      use: {
        baseURL: `http://127.0.0.1:${port}`,
      },
    },
    {
      name: "base-path",
      testMatch: /base-path\.spec\.js/,
      use: {
        baseURL: `http://127.0.0.1:${basePathPort}`,
      },
    },
  ],
  webServer: [
    {
      command: `pnpm exec vite dev --host 127.0.0.1 --port ${port}`,
      port,
      timeout: 120000,
      reuseExistingServer: !process.env.CI,
    },
    {
      command: `pnpm exec vite dev --host 127.0.0.1 --port ${basePathPort}`,
      env: {
        ...process.env,
        OAR_UI_BASE_PATH: appBasePath,
      },
      port: basePathPort,
      timeout: 120000,
      reuseExistingServer: !process.env.CI,
    },
  ],
});
