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

import { deferred, installWorkspaceApi } from "../helpers/workspaceApiMock.js";
import { waitForAppReady } from "../helpers/pageReady.js";

const BEFORE = process.env.FIRST_RUN_CAPTURE_BEFORE === "1";
const SHOTS = `.screenshots/review/first-run${BEFORE ? "-before" : ""}`;

const WS = "/o/local/w/local";

/** A workspace that was provisioned and never used. */
function snapshot({ agents = [] } = {}) {
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
    dashboard: { status: "ok", pinned_ref: null, has_more: false, reports: [] },
    agents: { status: "ok", truncated: false, items: agents },
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
      return route.fulfill({ json: snapshot({ agents: state.agents }) });
    if (path === "/stream/events") {
      const onFailed = (failed) => {
        if (failed === request) state.live.resolve();
      };
      page.on("requestfailed", onFailed);
      await state.live.promise;
      page.off("requestfailed", onFailed);
      const event = {
        id: "evt-first-agent",
        type: "agent_registered",
        ts: new Date().toISOString(),
        refs: ["agent:claude"],
      };
      return route
        .fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: `id: ${event.id}\nevent: event\ndata: ${JSON.stringify({ event })}\n\n`,
        })
        .catch(() => {});
    }
    return route.fallback();
  });

  /** The first agent checks in, and the live stream says so. */
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
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();

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

  // The first agent registers. Nothing is reloaded; the page notices.
  workspace.arrive();
  if (!BEFORE) {
    await expect(page.locator("[data-first-agent-connected]")).toContainText(
      /connected/i,
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

test("the rest of the first-run path", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mkdir(SHOTS, { recursive: true });
  await installEmptyWorkspace(page);

  await page.goto(`${WS}/tasks`);
  await waitForAppReady(page);
  await expect(page.getByText("Nothing tracked yet")).toBeVisible();
  await page.screenshot({ path: `${SHOTS}/2-tasks.png`, fullPage: true });

  await page.goto(`${WS}/inbox`);
  await waitForAppReady(page);
  await expect(page.locator("[data-inbox-empty]")).toBeVisible();
  await page.screenshot({ path: `${SHOTS}/3-inbox.png`, fullPage: true });

  await page.goto(`${WS}/agents`);
  await waitForAppReady(page);
  await expect(page.locator("[data-agents-empty]")).toBeVisible();
  await page.screenshot({ path: `${SHOTS}/4-agents.png`, fullPage: true });
});
