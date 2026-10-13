/**
 * The first-run path: a new signup lands in a workspace nothing has used yet.
 *
 * Captures the surfaces that first visit actually goes through, and asserts
 * the one thing a cold Overview has to do — offer connecting the first agent,
 * say it is waiting, and get out of the way the moment one arrives.
 *
 * The prompt the panel hands over is unit-covered (`firstAgentPanel.test.js`):
 * it turns on the deployment's API address, and this suite runs against a
 * loopback core, where the panel deliberately withholds it and offers the
 * commands instead.
 */
import { mkdir } from "node:fs/promises";

import { expect, test } from "@playwright/test";

import { holdOpenStream } from "../helpers/openStream.js";
import { deferred, installWorkspaceApi } from "../helpers/workspaceApiMock.js";
import { waitForAppReady } from "../helpers/pageReady.js";

const BEFORE = process.env.FIRST_RUN_CAPTURE_BEFORE === "1";
const SHOTS = `.screenshots/review/first-run${BEFORE ? "-before" : ""}`;

const WS = "/o/local/w/local";

/*
 * Every test here opens a workspace route the dev server has not compiled
 * yet, and they run in parallel: four cold compiles at once take well past
 * the suite's 10s default. The budget below covers the compile, not the
 * behaviour — each assertion about the behaviour keeps the default.
 */
const FIRST_PAINT = { timeout: 45_000 };

// eslint-disable-next-line no-empty-pattern
test.beforeEach(async ({}, testInfo) => {
  testInfo.setTimeout(120_000);
});

/** A workspace that was provisioned and never used. */
function snapshot({ agents = [], dashboard = null, truncatedAgents } = {}) {
  return {
    generated_at: new Date().toISOString(),
    work: { status: "ok", items: [], human_count: 0, truncated: false },
    initiatives: { status: "ok", count: 0, items: [] },
    needs_you: {
      status: "ok",
      count: 0,
      rows: [],
      href: "/inbox?mailbox=needs-you",
    },
    dashboard: dashboard ?? {
      status: "ok",
      pinned_ref: null,
      has_more: false,
      reports: [],
    },
    agents: {
      status: "ok",
      truncated: Boolean(truncatedAgents),
      items: agents,
    },
  };
}

/**
 * @param {import("@playwright/test").Page} page
 */
async function installEmptyWorkspace(page) {
  const state = {
    agents: /** @type {object[]} */ ([]),
    /** Released by `arrive()`, which is what makes the flip deterministic. */
    live: deferred(),
    /** How many times the roster stream has been connected. */
    rosterConnects: 0,
    /** The arrival is announced once; later reconnects stay open. */
    delivered: false,
  };
  const api = await installWorkspaceApi(page, {
    actors: [{ id: "operator", display_name: "Alex", tags: ["human"] }],
    principals: [{ actor_id: "operator", principal_kind: "human" }],
    agents: [],
    hosts: [],
    work: [],
    // A workspace with no machine in it has no PM either.
    pm: {
      state: "not_configured",
      configured: false,
      connected: false,
      last_seen: null,
    },
  });
  await page.route("**/*", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/overview")
      return route.fulfill({
        json: snapshot({
          agents: state.agents,
          dashboard: state.dashboard,
          truncatedAgents: state.truncatedAgents,
        }),
      });
    /*
     * The roster hub, as core drives it. `CompleteHeadlessHostEnrollment` and
     * the first `host_assertion` token both call `agentChanges.publish()` and
     * append nothing to the event log, so `agents_changed` here is the only
     * notice a browser gets that the first agent exists. Core sends one on
     * connect and one per change, with no `id:` and no replay cursor.
     */
    if (path === "/stream/agents") {
      state.rosterConnects += 1;
      const revision = state.rosterConnects;
      if (state.rosterConnects > 1) {
        /*
         * A finished SSE body is a drop, and the client reconnects. Announce
         * the arrival once and hold every connection after it open, the way
         * core does between changes — otherwise the resolved waiter would
         * re-announce it every three seconds.
         */
        if (state.delivered) return holdOpenStream(page, route);
        const onFailed = (failed) => {
          if (failed === request) state.live.resolve();
        };
        page.on("requestfailed", onFailed);
        await state.live.promise;
        page.off("requestfailed", onFailed);
        state.delivered = true;
      }
      return route
        .fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: `event: agents_changed\ndata: ${JSON.stringify({ revision })}\n\n`,
        })
        .catch(() => {});
    }
    /*
     * `/stream/events` is deliberately not handled here: it falls through to
     * the shared mock, which holds it open and silent. That is what core does
     * on a first enrollment, and it is the point of this spec — whatever flips
     * the page must not be riding on the workspace event log.
     */
    return route.fallback();
  });

  /**
   * The first agent checks in: `GET /agents` now has a row, and the roster
   * hub says so. Nothing is appended to the workspace event log, because core
   * appends nothing there either.
   */
  state.arrive = () => {
    state.agents = [
      {
        id: "agent-1",
        display_name: "claude",
        state: "idle",
        last_signal_at: new Date().toISOString(),
      },
    ];
    api.agents = state.agents;
    state.live.resolve();
  };
  return state;
}

test("a cold Overview leads with connecting the first agent", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mkdir(SHOTS, { recursive: true });
  const workspace = await installEmptyWorkspace(page);

  await page.goto(`${WS}/overview`);
  await waitForAppReady(page);
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible(
    FIRST_PAINT,
  );

  const panel = page.locator("[data-first-agent-panel]");
  if (BEFORE) {
    // What this spec exists to change: the landing page of an unused
    // workspace offered nothing to do but pin a report that cannot exist yet.
    await expect(panel).toHaveCount(0);
  } else {
    await expect(
      page.getByRole("heading", { name: "Connect your first agent" }),
    ).toBeVisible();
    await expect(panel.getByText(/Claude Code, Codex or Cursor/)).toBeVisible();
    // Live, and it says so: the reader pastes the prompt on another machine
    // and comes back to this tab.
    await expect(page.locator("[data-first-agent-waiting]")).toContainText(
      /waiting for your first agent/i,
    );
    // The empty sections the panel stands in for are not also on screen.
    await expect(
      page.locator('[data-overview-section="initiatives"]'),
    ).toHaveCount(0);
    await expect(page.locator('[data-overview-section="reports"]')).toHaveCount(
      0,
    );
  }
  await page.screenshot({ path: `${SHOTS}/1-overview.png`, fullPage: true });

  /*
   * The first agent registers, exactly as core reports it: a row in
   * `GET /agents` and an `agents_changed` on the roster hub. The workspace
   * event log stays silent, which is what core leaves it at. Nothing is
   * reloaded and nothing is navigated — the visible tab notices.
   */
  workspace.arrive();
  if (!BEFORE) {
    await expect(page.locator("[data-first-agent-connected]")).toContainText(
      /connected/i,
      // The roster stream reconnects on its own backoff before it can carry
      // the news; the flip is bounded by that, not by a poll.
      { timeout: 20_000 },
    );
    await expect(panel).toHaveCount(0);
    await expect(
      page.locator('[data-overview-section="initiatives"]'),
    ).toBeVisible();
    await page.screenshot({
      path: `${SHOTS}/5-first-agent-connected.png`,
      fullPage: true,
    });
  }
});

test("a workspace with a pinned dashboard is not cold", async ({ page }) => {
  // Every agent revoked and every task archived still leaves a workspace
  // somebody has used. Its Overview is the Overview, not an invitation to
  // connect a first agent.
  const workspace = await installEmptyWorkspace(page);
  workspace.dashboard = {
    status: "ok",
    pinned_ref: "document:dashboard",
    has_more: false,
    reports: [],
  };

  await page.goto(`${WS}/overview`);
  await waitForAppReady(page);
  await expect(
    page.locator('[data-overview-section="initiatives"]'),
  ).toBeVisible(FIRST_PAINT);
  await expect(page.locator("[data-first-agent-panel]")).toHaveCount(0);
});

test("a truncated roster is never read as no agents", async ({ page }) => {
  // `truncated` means core stopped short of the end. A zero beside it counts
  // the window it read, not the workspace.
  const workspace = await installEmptyWorkspace(page);
  workspace.truncatedAgents = true;

  await page.goto(`${WS}/overview`);
  await waitForAppReady(page);
  await expect(
    page.locator('[data-overview-section="initiatives"]'),
  ).toBeVisible(FIRST_PAINT);
  await expect(page.locator("[data-first-agent-panel]")).toHaveCount(0);
});

test("both setup paths are reachable on a 320px screen", async ({ page }) => {
  // The narrowest phone the shell supports. A tab painted past the right edge
  // is a path the reader cannot take, and "Run it myself" is the one that
  // still works on a deployment that cannot hand over a prompt.
  await page.setViewportSize({ width: 320, height: 900 });
  await mkdir(SHOTS, { recursive: true });
  await installEmptyWorkspace(page);

  await page.goto(`${WS}/overview`);
  await waitForAppReady(page);
  const tabs = page.locator("[data-setup-tab]");
  await expect(tabs).toHaveCount(2, FIRST_PAINT);
  /*
   * Each label has to be legible in full, not merely inside the viewport: a
   * flex row that refuses to wrap keeps both boxes on screen by shrinking
   * them until the words are cut off, which is the same dead end.
   */
  const cut = await tabs.evaluateAll((nodes) =>
    nodes
      .filter((node) => node.scrollWidth > node.clientWidth + 1)
      .map(
        (node) =>
          `${node.textContent?.trim()} (${node.clientWidth}px of ${node.scrollWidth}px)`,
      ),
  );
  expect(cut, "setup tab labels cut off at 320px").toEqual([]);
  for (const tab of await tabs.all()) {
    const box = await tab.boundingBox();
    expect(box, await tab.innerText()).not.toBeNull();
    expect(box.x + box.width, await tab.innerText()).toBeLessThanOrEqual(320);
  }
  await page.screenshot({ path: `${SHOTS}/6-narrow.png`, fullPage: true });
});

test("the rest of the first-run path", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mkdir(SHOTS, { recursive: true });
  await installEmptyWorkspace(page);

  await page.goto(`${WS}/tasks`);
  await waitForAppReady(page);
  await expect(page.getByText("Nothing tracked yet")).toBeVisible(FIRST_PAINT);
  await page.screenshot({ path: `${SHOTS}/2-tasks.png`, fullPage: true });

  await page.goto(`${WS}/inbox`);
  await waitForAppReady(page);
  await expect(page.locator("[data-inbox-empty]")).toBeVisible(FIRST_PAINT);
  await page.screenshot({ path: `${SHOTS}/3-inbox.png`, fullPage: true });

  await page.goto(`${WS}/agents`);
  await waitForAppReady(page);
  await expect(page.locator("[data-agents-empty]")).toBeVisible(FIRST_PAINT);
  await page.screenshot({ path: `${SHOTS}/4-agents.png`, fullPage: true });
});
