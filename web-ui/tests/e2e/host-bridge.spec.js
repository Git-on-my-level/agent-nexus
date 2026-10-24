import { expect, test } from "@playwright/test";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";
const BASE = "/o/local/w/local";
let browser;
test.beforeAll(async ({ browser: sharedBrowser }) => {
  browser = sharedBrowser;
});
// eslint-disable-next-line no-empty-pattern
test("machine bridge liveness ages, recovers and stays distinct from wake coverage", async ({}, testInfo) => {
  const context = await browser.newContext({
    baseURL: testInfo.project.use.baseURL,
  });
  const page = await context.newPage();
  try {
    const now = Date.now();
    await page.clock.install({ time: new Date(now) });
    const hosts = ["live", "stale", "none", "zero"].map((id) => ({
      id,
      slug: id,
      display_name: `Machine ${id}`,
      os_user: "operator",
      hostname: id,
      created_at: new Date(now).toISOString(),
      revoked_at: null,
      agents: [],
      excluded_names: [],
      discovered_adapters: [],
    }));
    const observation = (id, expires) => ({
      host_id: id,
      revoked_at: null,
      bridge:
        expires === null
          ? null
          : {
              host_id: id,
              instance_id: `instance-${id}`,
              sequence: 1,
              version: "test",
              last_seen_at: new Date(now - 1000).toISOString(),
              expires_at: new Date(expires).toISOString(),
            },
      agents:
        id === "live"
          ? [
              {
                actor_id: "actor-worker",
                jobs: [
                  { kind: "answers", enabled: true, ready: true },
                  { kind: "files", enabled: true, ready: true },
                ],
              },
            ]
          : [],
      agents_truncated: false,
    });
    const snapshots = {
      live: observation("live", now + 120000),
      stale: observation("stale", now - 1),
      none: observation("none", null),
      zero: observation("zero", now + 3600000),
    };
    const agent = {
      id: "worker",
      actor_id: "actor-worker",
      ref: "actor:actor-worker",
      host_id: "live",
      host_slug: "live",
      name: "worker",
      handle: "worker.live",
      display_name: "worker on live",
      identity_kind: "derived",
      state: "idle",
      bridge_online: false,
      last_signal_at: new Date(now).toISOString(),
      open_asks_count: 0,
      revoked_at: null,
    };
    await installWorkspaceApi(page, { hosts, agents: [agent] });
    const batches = [];
    await page.route("**/hosts/bridge-status", async (route) => {
      const ids = route.request().postDataJSON().host_ids;
      batches.push(ids);
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          as_of: new Date().toISOString(),
          hosts: ids.map((id) => snapshots[id]).filter(Boolean),
        }),
      });
    });
    await page.goto(`${BASE}/agents`);
    const badge = (id) => page.locator(`[data-host-bridge="${id}"]`);
    await expect(badge("live")).toHaveAttribute("data-bridge-state", "live");
    await expect(badge("stale")).toHaveAttribute("data-bridge-state", "stale");
    await expect(badge("none")).toContainText("No bridge");
    await expect(badge("zero")).toHaveAttribute("data-bridge-state", "live");
    await expect(page.locator("[data-bridge-fleet-summary]")).toHaveText(
      "2 of 4 machines live on the bridge",
    );
    const zero = snapshots.zero;
    delete snapshots.zero;
    await page.evaluate(() =>
      document.dispatchEvent(new Event("visibilitychange")),
    );
    await expect(badge("zero")).toHaveAttribute("data-bridge-state", "unknown");
    await expect(page.locator("[data-bridge-fleet-summary]")).toHaveText(
      "1 of 3 machines live on the bridge",
    );
    snapshots.zero = zero;
    await page.evaluate(() =>
      document.dispatchEvent(new Event("visibilitychange")),
    );
    await expect(badge("zero")).toHaveAttribute("data-bridge-state", "live");
    await expect(badge("live")).toContainText("Live on bridge");
    await badge("live").locator("summary").click();
    await expect(badge("live")).toContainText("File sync");
    await expect(badge("none")).toContainText("anx bridge install");
    await expect(badge("live")).not.toContainText("anx bridge install");
    await expect(
      page.locator('[data-agent-bridge="offline"]').first(),
    ).toBeVisible();
    expect(batches.some((ids) => ids.length === 4)).toBe(true);
    await page.clock.fastForward(121000);
    await expect(badge("live")).toHaveAttribute("data-bridge-state", "stale");
    snapshots.live.bridge.expires_at = new Date(now + 3600000).toISOString();
    await page.evaluate(() =>
      document.dispatchEvent(new Event("visibilitychange")),
    );
    await expect(badge("live")).toHaveAttribute("data-bridge-state", "live");
    snapshots.live.revoked_at = new Date(now).toISOString();
    await page.evaluate(() =>
      document.dispatchEvent(new Event("visibilitychange")),
    );
    await expect(badge("live")).toHaveAttribute("data-bridge-state", "revoked");
    // Badges ask for status only once they are near the viewport. Put the
    // last card below the fold so the check never depends on font metrics
    // or banners above it, then read it as a reader would: scrolled to.
    await page.setViewportSize({ width: 1280, height: 540 });
    await page.goto(`${BASE}/access`);
    await badge("zero").scrollIntoViewIfNeeded();
    await expect(badge("zero")).toHaveAttribute("data-bridge-state", "live");
    await expect(badge("none")).toContainText("No bridge");
  } finally {
    await context.close();
  }
});

// eslint-disable-next-line no-empty-pattern
test("an old core shows checking then unavailable without inventing bridge absence", async ({}, testInfo) => {
  const context = await browser.newContext({
    baseURL: testInfo.project.use.baseURL,
  });
  const page = await context.newPage();
  let release;
  const responseGate = new Promise((resolve) => {
    release = resolve;
  });
  try {
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    const host = {
      id: "old-core",
      slug: "old-core",
      display_name: "Older core machine",
      os_user: "operator",
      hostname: "old-core",
      created_at: new Date().toISOString(),
      revoked_at: null,
      agents: [],
      excluded_names: [],
      discovered_adapters: [],
    };
    await installWorkspaceApi(page, { hosts: [host], agents: [] });
    await page.route("**/hosts/bridge-status", async (route) => {
      await responseGate;
      await route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({
          error: { code: "not_found", message: "Not found" },
        }),
      });
    });
    await page.goto(`${BASE}/agents`);
    const badge = page.locator('[data-host-bridge="old-core"]');
    await expect(badge).toHaveAttribute("data-bridge-state", "loading");
    await expect(badge).toContainText("Checking bridge…");
    await expect(badge).not.toContainText("Bridge unavailable");
    release();
    await expect(badge).toHaveAttribute("data-bridge-state", "unknown");
    await expect(badge).toContainText("Bridge unavailable");
    await expect(badge).not.toContainText("No bridge");
    await expect(page.locator("[data-bridge-fleet-summary]")).toHaveCount(0);
    await page.goto(`${BASE}/access`);
    await expect(badge).toHaveAttribute("data-bridge-state", "unknown");
    expect(errors).toEqual([]);
  } finally {
    release();
    await context.close();
  }
});
