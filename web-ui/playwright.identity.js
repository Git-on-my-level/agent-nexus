import { assertServerIdentity } from "./e2eIdentity.js";
import { resolvePort } from "./playwright.ports.js";

/**
 * Playwright `globalSetup`: refuse to run the suite against a web-ui server
 * that was not started from this checkout. See e2eIdentity.js for why.
 *
 * Runs after the webServer plugins have started (or adopted) each server, so a
 * reused server is already bound by the time this checks it.
 */
export default async function globalSetup() {
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
}
