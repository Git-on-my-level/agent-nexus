import { defineConfig } from "@playwright/test";

const port = Number(process.env.PLAYWRIGHT_PORT ?? 4173);
// Default avoids clashing with a stray preview/other dev server on 4174.
const basePathPort = Number(process.env.PLAYWRIGHT_BASE_PATH_PORT ?? 4176);
const corePort = Number(process.env.PLAYWRIGHT_CORE_PORT ?? 8000);
const appBasePath = process.env.PLAYWRIGHT_APP_BASE_PATH ?? "/anx";
const preview =
  process.env.PLAYWRIGHT_PREVIEW === "1" ||
  process.env.PLAYWRIGHT_PREVIEW === "true";
const mockedCoreBaseUrl =
  process.env.PLAYWRIGHT_CORE_BASE_URL ?? `http://127.0.0.1:${corePort}`;

const coreWebServer = {
  command: `rm -rf /tmp/anx-playwright-core-workspace && cd ../core && HOST=127.0.0.1 PORT=${corePort} WORKSPACE_ROOT=/tmp/anx-playwright-core-workspace ./scripts/dev`,
  port: corePort,
  timeout: 120000,
  reuseExistingServer: !process.env.CI,
};

const defaultWorkspaceEnv = () => ({
  ...process.env,
  ANX_WORKSPACES:
    process.env.ANX_WORKSPACES ??
    JSON.stringify([
      {
        organizationSlug: "local",
        slug: "local",
        label: "Local",
        coreBaseUrl: mockedCoreBaseUrl,
      },
    ]),
  ANX_DEFAULT_ORGANIZATION: process.env.ANX_DEFAULT_ORGANIZATION ?? "local",
  ANX_DEFAULT_WORKSPACE: process.env.ANX_DEFAULT_WORKSPACE ?? "local",
  ANX_UI_SKIP_CORE_SCHEMA_CHECK:
    process.env.ANX_UI_SKIP_CORE_SCHEMA_CHECK ?? "1",
});

const webServer = preview
  ? [
      coreWebServer,
      {
        command: `pnpm exec vite build && pnpm exec vite preview --host 127.0.0.1 --port ${port} --strictPort`,
        env: defaultWorkspaceEnv(),
        port,
        timeout: 300000,
        reuseExistingServer: !process.env.CI,
      },
    ]
  : [
      coreWebServer,
      {
        command: `pnpm exec vite dev --host 127.0.0.1 --port ${port}`,
        env: defaultWorkspaceEnv(),
        port,
        timeout: 120000,
        // Always bounce Vite for e2e: hooks.server/ssr logic must match the checkout (reuse can serve stale SSR).
        reuseExistingServer:
          process.env.PLAYWRIGHT_REUSE_EXISTING_WEB_UI === "1",
      },
      {
        command: `pnpm exec vite dev --host 127.0.0.1 --port ${basePathPort}`,
        env: {
          ...defaultWorkspaceEnv(),
          ANX_UI_BASE_PATH: appBasePath,
        },
        port: basePathPort,
        timeout: 120000,
        reuseExistingServer:
          process.env.PLAYWRIGHT_REUSE_EXISTING_WEB_UI === "1" ||
          !process.env.CI,
      },
    ];

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
  webServer,
});
