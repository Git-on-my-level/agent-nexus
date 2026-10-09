import { defineConfig } from "@playwright/test";

import {
  isCi,
  localWorkers,
  resolvePort,
  reuseExistingServer,
} from "./playwright.ports.js";

// Ports come from this worktree's block unless CI or the caller pins them.
const port = resolvePort("webUi", "PLAYWRIGHT_PORT");
const basePathPort = resolvePort("basePath", "PLAYWRIGHT_BASE_PATH_PORT");
const corePort = resolvePort("core", "PLAYWRIGHT_CORE_PORT");
const appBasePath = process.env.PLAYWRIGHT_APP_BASE_PATH ?? "/anx";
const preview =
  process.env.PLAYWRIGHT_PREVIEW === "1" ||
  process.env.PLAYWRIGHT_PREVIEW === "true";
// Opt-in: run against an installed browser (e.g. PLAYWRIGHT_CHANNEL=chrome) instead of the bundled Chromium download.
const channel = process.env.PLAYWRIGHT_CHANNEL || undefined;
const mockedCoreBaseUrl =
  process.env.PLAYWRIGHT_CORE_BASE_URL ?? `http://127.0.0.1:${corePort}`;

// Keyed by port so concurrent runs (different PLAYWRIGHT_CORE_PORT) do not wipe
// each other's core workspace.
const coreWorkspaceRoot =
  process.env.PLAYWRIGHT_CORE_WORKSPACE_ROOT ??
  `/tmp/anx-playwright-core-workspace-${corePort}`;

const coreWebServer = {
  command: `rm -rf ${coreWorkspaceRoot} && cd ../core && HOST=127.0.0.1 PORT=${corePort} WORKSPACE_ROOT=${coreWorkspaceRoot} ./scripts/dev`,
  env: {
    ANX_BOOTSTRAP_TOKEN:
      process.env.ANX_BOOTSTRAP_TOKEN ?? "playwright-local-bootstrap-token",
  },
  port: corePort,
  timeout: 120000,
  reuseExistingServer: reuseExistingServer("PLAYWRIGHT_REUSE_EXISTING_CORE"),
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
  // Parallel workers load the same URLs at once, which the per-URL
  // navigation-loop guard would otherwise short-circuit.
  ANX_UI_DISABLE_REQUEST_LOOP_GUARD:
    process.env.ANX_UI_DISABLE_REQUEST_LOOP_GUARD ?? "1",
});

const webServer = preview
  ? [
      coreWebServer,
      {
        command: `pnpm exec vite build && pnpm exec vite preview --host 127.0.0.1 --port ${port} --strictPort`,
        env: defaultWorkspaceEnv(),
        port,
        timeout: 300000,
        reuseExistingServer: reuseExistingServer(
          "PLAYWRIGHT_REUSE_EXISTING_WEB_UI",
        ),
      },
    ]
  : [
      coreWebServer,
      {
        // --strictPort: a taken port must fail loudly, not move the server
        // somewhere Playwright is not waiting.
        command: `pnpm exec vite dev --host 127.0.0.1 --port ${port} --strictPort`,
        env: defaultWorkspaceEnv(),
        port,
        timeout: 120000,
        // Always bounce Vite for e2e: hooks.server/ssr logic must match the checkout (reuse can serve stale SSR).
        reuseExistingServer: reuseExistingServer(
          "PLAYWRIGHT_REUSE_EXISTING_WEB_UI",
        ),
      },
      {
        command: `pnpm exec vite dev --host 127.0.0.1 --port ${basePathPort} --strictPort`,
        env: {
          ...defaultWorkspaceEnv(),
          ANX_UI_BASE_PATH: appBasePath,
        },
        port: basePathPort,
        timeout: 120000,
        reuseExistingServer: reuseExistingServer(
          "PLAYWRIGHT_REUSE_EXISTING_WEB_UI",
        ),
      },
    ];

export default defineConfig({
  testDir: "tests/e2e",
  fullyParallel: true,
  forbidOnly: isCi,
  retries: isCi ? 2 : 0,
  workers: isCi ? 1 : localWorkers,
  reporter: "list",
  // The dev server compiles routes on first hit; 5s is too tight for a cold page.
  expect: { timeout: 10_000 },
  use: {
    headless: true,
    channel,
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
