import { defineConfig } from "@playwright/test";

// Isolated, synthetic UI qualification. Never starts or mutates a real workspace.
const port = Number(process.env.PM_UI_TEST_PORT || 4381);
const corePort = Number(process.env.PM_CORE_TEST_PORT || 4382);
export default defineConfig({
  testDir: "tests/e2e",
  testMatch: "pm-work.spec.js",
  workers: 1,
  retries: 0,
  outputDir: "test-results/pm",
  reporter: "list",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    headless: true,
    trace: "retain-on-failure",
  },
  webServer: [
    {
      command: "node tests/fixtures/pm-core.js",
      env: { ...process.env, PM_CORE_TEST_PORT: String(corePort) },
      port: corePort,
      reuseExistingServer: false,
    },
    {
      command: `pnpm exec vite dev --host 127.0.0.1 --port ${port} --strictPort`,
      env: {
        ...process.env,
        ANX_CORE_BASE_URL: `http://127.0.0.1:${corePort}`,
        ANX_WORKSPACES: JSON.stringify([
          {
            organizationSlug: "local",
            slug: "local",
            label: "Synthetic QA",
            coreBaseUrl: `http://127.0.0.1:${corePort}`,
          },
        ]),
        ANX_DEFAULT_ORGANIZATION: "local",
        ANX_DEFAULT_WORKSPACE: "local",
      },
      port,
      reuseExistingServer: false,
    },
  ],
});
