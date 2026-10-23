import { assertServerIdentity } from "./e2eIdentity.js";
import { resolvePort } from "./playwright.ports.js";
import { chromium } from "@playwright/test";
import { installWorkspaceApi } from "./tests/helpers/workspaceApiMock.js";

/**
 * Playwright `globalSetup`: refuse to run the suite against a web-ui server
 * that was not started from this checkout. See e2eIdentity.js for why.
 *
 * Runs after the webServer plugins have started (or adopted) each server, so a
 * reused server is already bound by the time this checks it.
 */
export default async function globalSetup(config) {
  const preview =
    process.env.PLAYWRIGHT_PREVIEW === "1" ||
    process.env.PLAYWRIGHT_PREVIEW === "true";
  const targets = [
    {
      role: "web-ui",
      port: resolvePort("webUi", "PLAYWRIGHT_PORT"),
      variable: "PLAYWRIGHT_PORT",
      basePath: "",
    },
  ];
  // Preview mode serves one built app and starts no base-path server.
  if (!preview) {
    targets.push({
      role: "base-path web-ui",
      port: resolvePort("basePath", "PLAYWRIGHT_BASE_PATH_PORT"),
      variable: "PLAYWRIGHT_BASE_PATH_PORT",
      basePath: process.env.PLAYWRIGHT_APP_BASE_PATH ?? "/anx",
    });
  }
  await assertServerIdentity(targets);
  await warmWorkspacePages(targets, {
    channel:
      config?.projects?.[0]?.use?.channel ?? process.env.PLAYWRIGHT_CHANNEL,
  });
}

// A listening socket (and the lightweight identity middleware) says nothing
// about Vite's first client compile. Pay that cost in server preparation, before
// a spec's 10s assertions start. Use a disposable, mocked workspace so startup
// never enrolls a principal or changes the shared core fixture.
export async function warmWorkspacePages(
  targets,
  { channel, launchBrowser = (options) => chromium.launch(options) } = {},
) {
  const browser = await launchBrowser({ headless: true, channel });
  try {
    for (const { port, basePath = "" } of targets) {
      const page = await browser.newPage();
      try {
        await installWorkspaceApi(page, {}, { basePath });
        // This is the existing dev-server startup budget, not a spec timeout.
        page.setDefaultTimeout(120_000);
        await page.goto(
          `http://127.0.0.1:${port}${basePath}/o/local/w/local/agents`,
          { waitUntil: "domcontentloaded" },
        );
        await page
          .getByRole("heading", { name: "Agents", exact: true })
          .waitFor();
      } finally {
        await page.close();
      }
    }
  } finally {
    await browser.close();
  }
}
