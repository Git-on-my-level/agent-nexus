import { realpathSync } from "node:fs";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Proves the web-ui server an e2e run is about to drive was started from this
 * checkout.
 *
 * Playwright's `reuseExistingServer` and the `PLAYWRIGHT_PORT` /
 * `PLAYWRIGHT_BASE_PATH_PORT` overrides only check that *something* is
 * listening. Point a run at another worktree's dev server — or at the
 * base-path server instead of the plain one — and every selector misses, so
 * the suite reports "element(s) not found" and reads as a page regression on
 * the branch under test. SCA-767: four `docs-states.spec.js` "detail:" tests
 * were filed as a `main` regression on exactly that evidence; they pass on
 * `main`. One wrong port cost an issue and a sprint slot, so the harness now
 * says whose server it found instead of letting the specs guess.
 *
 * The identity route is dev/preview middleware (see vite.config.js), so it
 * never reaches a built bundle.
 */
export const E2E_IDENTITY_PATH = "/__anx/e2e-identity";

export const WEB_UI_ROOT = realpathSync(
  dirname(fileURLToPath(import.meta.url)),
);

/**
 * Compare what a server reported against what this checkout expects. Pure, so
 * the failure text is unit-tested rather than discovered during an outage.
 *
 * @returns {string | null} an actionable message, or null when the server is ours.
 */
export function identityMismatch({
  role,
  port,
  variable,
  basePath = "",
  expected,
  actual,
}) {
  const where = `${role} server on port ${port}`;
  const fix =
    `Unset PLAYWRIGHT_REUSE_EXISTING_WEB_UI and ${variable}, or restart the ` +
    `server from ${expected}, then re-run. Results from another checkout are ` +
    `not evidence about this one.`;

  if (!actual) {
    return (
      `${where} does not serve ${E2E_IDENTITY_PATH}, so this run cannot prove ` +
      `it is driving ${expected}. Something else is listening on that port ` +
      `(an older checkout, a plain static server, another app). ${fix}`
    );
  }
  if (actual.root !== expected) {
    return (
      `${where} was started from ${actual.root} (pid ${actual.pid}), not ` +
      `${expected}. The suite would have tested that checkout's code. ${fix}`
    );
  }
  const actualBasePath = actual.basePath ?? "";
  if (actualBasePath !== basePath) {
    return (
      `${where} serves the app under base path "${actualBasePath || "/"}", ` +
      `but this project expects "${basePath || "/"}". The plain and base-path ` +
      `servers are swapped, so every route under test is the wrong one. ${fix}`
    );
  }
  return null;
}

/**
 * Read a server's identity, retrying while it finishes booting. Returns null
 * once the window closes: a server that cannot identify itself is a finding,
 * not an error to propagate.
 */
async function fetchIdentity(port, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(
        `http://127.0.0.1:${port}${E2E_IDENTITY_PATH}`,
        { signal: AbortSignal.timeout(2000) },
      );
      if (response.ok) {
        // A foreign server answering every path with HTML lands in the catch.
        try {
          const parsed = JSON.parse(await response.text());
          return typeof parsed?.root === "string" ? parsed : null;
        } catch {
          return null;
        }
      }
    } catch {
      // Not up yet, or not listening at all; retry until the deadline.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  return null;
}

/**
 * Playwright `globalSetup`. Runs after the webServer plugins have started or
 * adopted each server, and before the first spec.
 */
export async function assertServerIdentity(
  targets,
  { timeoutMs = 15_000 } = {},
) {
  const problems = [];
  for (const target of targets) {
    const actual = await fetchIdentity(target.port, timeoutMs);
    const problem = identityMismatch({
      ...target,
      expected: WEB_UI_ROOT,
      actual,
    });
    if (problem) problems.push(problem);
  }
  if (problems.length) {
    throw new Error(
      `e2e server identity check failed:\n\n` +
        problems.map((line) => `  - ${line}`).join("\n\n") +
        `\n`,
    );
  }
}
