import { expect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * The Agents roster and agent page against a mocked roster: every derived
 * state, the Inbox hand-off for waiting agents, keyboard use, run
 * attribution, and the layout audit at each viewport.
 */

const BASE = "/o/local/w/local";
const HOST_ID = "host_1";
const NOW = Date.now();
const ago = (minutes) => new Date(NOW - minutes * 60_000).toISOString();

function agent(name, state, overrides = {}) {
  return {
    id: `agent-${name}`,
    ref: `actor:actor-${name}`,
    actor_id: `actor-${name}`,
    host_id: HOST_ID,
    host_slug: "m5-mbp",
    name,
    handle: `${name}.m5-mbp`,
    display_name: `${name} on m5-mbp`,
    identity_kind: "derived",
    state,
    bridge_online: false,
    current_card_ref: null,
    current_card_title: null,
    last_progress_note: null,
    last_progress_at: null,
    active_run: null,
    open_asks_count: 0,
    last_signal_at: ago(3),
    revoked_at: null,
    ...overrides,
  };
}

const LEO = agent("codex", "working", {
  bridge_online: true,
  current_card_ref: "card:tune-core-combat-loop",
  current_card_title:
    "Tune core combat loop for the vertical slice with a title long enough to truncate",
  last_progress_note: "Parry window at 120 ms, testing input buffer",
  last_progress_at: ago(6),
  active_run: {
    run_id: "run-1",
    adapter: "codex",
    model: "sol",
    duration_seconds: 840,
  },
});
const OMAR = agent("claude", "waiting_on_human", { open_asks_count: 1 });
const IDLE = agent("reviewer", "idle", { last_signal_at: ago(120) });
const STALE = agent("release-bot", "stale", { last_signal_at: null });
const ROSTER = [STALE, IDLE, LEO, OMAR];

const INBOX_ITEM = {
  id: "inbox:ask:thread-1:evt-ask:evt-ask",
  kind: "ask",
  title: "Confirm 20-minute quest path",
  requester_actor_id: OMAR.actor_id,
  requester_label: "Omar Reed",
  severity: "high",
  source_event_id: "evt-ask",
  source_event_time: ago(192),
  subject_ref: "card:lock-hub-quest-path",
  related_refs: [],
  response_proposals: ["Yes"],
};

const RUN = {
  id: "run-1",
  ref: "run:exec-7f3a",
  handle: "exec-7f3a",
  launcher: "agentctl",
  external_id: "exec-7f3a",
  host_id: HOST_ID,
  agent_id: LEO.id,
  adapter: "codex",
  model: "sol",
  state: "running",
  liveness: "alive",
  result_collected: false,
  labels: ["anx.card.tune-core-combat-loop"],
  card_ref: "card:tune-core-combat-loop",
  started_at: ago(14),
  ended_at: null,
  last_observed_at: ago(1),
};

/** Page loads and SvelteKit data share these paths; only API calls are mocked. */
function isApiCall(route) {
  const request = route.request();
  return (
    ["fetch", "xhr", "eventsource"].includes(request.resourceType()) &&
    !request.url().includes("__data.json")
  );
}

async function installAgentsApi(page, overrides = {}) {
  const api = { roster: ROSTER, fail: {}, ...overrides };
  const mock = (pattern, handler) =>
    page.route(pattern, (route) =>
      isApiCall(route) ? handler(route) : route.fallback(),
    );
  const json = (route, status, body) =>
    route.fulfill({
      status,
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });

  // Shell endpoints (session, actors, principals, inbox sources) come from
  // the shared mock; the routes below take precedence for this surface.
  await installWorkspaceApi(page);
  await mock(/\/agents$/, (route) =>
    api.fail.roster
      ? json(route, 500, {
          error: { code: "internal", message: api.fail.roster },
        })
      : json(route, 200, { agents: api.roster }),
  );
  await mock(/\/agents\/[^/?]+$/, (route) => {
    const key = decodeURIComponent(route.request().url().split("/agents/")[1]);
    const found = api.roster.find(
      (entry) => entry.handle === key || entry.id === key,
    );
    if (!found) {
      return json(route, 404, {
        error: { code: "not_found", message: "agent not found" },
      });
    }
    return json(route, 200, {
      agent: found,
      recent_cards:
        found === LEO
          ? [
              {
                id: "card-uuid",
                ref: "card:tune-core-combat-loop",
                handle: "tune-core-combat-loop",
                title: "Tune core combat loop",
                column_key: "in_progress",
                updated_at: ago(30),
              },
            ]
          : [],
      recent_runs: found === LEO ? [RUN] : [],
      open_asks:
        found === OMAR
          ? [
              {
                id: "evt-ask",
                kind: "ask",
                title: INBOX_ITEM.title,
                severity: "high",
                subject_ref: "card:lock-hub-quest-path",
                subject_title: "Lock hub quest path",
                related_refs: [],
                response_proposals: ["Yes"],
              },
            ]
          : [],
      recent_notes:
        found === LEO
          ? [
              {
                text: LEO.last_progress_note,
                at: LEO.last_progress_at,
                card_ref: LEO.current_card_ref,
              },
            ]
          : [],
    });
  });
  await mock(/\/inbox(\?.*)?$/, (route) =>
    json(route, 200, { items: [INBOX_ITEM] }),
  );
  await page.route(/\/events\?.*actor_id=/, (route) =>
    json(route, 200, {
      events: [
        {
          id: "evt-msg",
          type: "message_posted",
          ts: ago(2),
          actor_id: LEO.actor_id,
          refs: ["card:tune-core-combat-loop"],
          payload: {
            text: "Input buffer set to 6 frames; capture build next.",
            subject_ref: "card:tune-core-combat-loop",
          },
          run_attribution: {
            run_id: "run-1",
            host_id: HOST_ID,
            agent_id: LEO.id,
            adapter: "codex",
          },
        },
      ],
    }),
  );
  await page.route(/\/runs\/run-1$/, (route) => json(route, 200, { run: RUN }));
  await page.route(/(?<!\/auth)\/hosts\/[^/?]+$/, (route) =>
    json(route, 200, {
      host: {
        id: HOST_ID,
        slug: "m5-mbp",
        display_name: "m5-mbp",
        os_user: "david",
        hostname: "m5-mbp.local",
        discovered_adapters: ["codex", "claude"],
        key_id: "hkey_1",
        excluded_names: [],
        agents: [],
        created_at: ago(60 * 24 * 3),
        revoked_at: null,
      },
    }),
  );
  return api;
}

test("roster groups agents by derived state and hands asks to the Inbox", async ({
  page,
}) => {
  await installAgentsApi(page);
  await page.goto(`${BASE}/agents`);
  const roster = page.locator("[data-agents-roster]");
  await expect(roster).toBeVisible();
  await expect(roster.locator("h2")).toHaveText([
    /Waiting on you\s*1/,
    /Working\s*1/,
    /Idle\s*1/,
    /Stale\s*1/,
  ]);
  await expect(page.locator("[data-agent-summary]")).toContainText(
    "1 waiting on you",
  );
  await expect(page.locator("[data-agents-nav-count]").first()).toHaveText("1");

  const working = page.locator('[data-agent-row="codex.m5-mbp"]');
  await expect(working).toContainText("codex sol");
  await expect(working).toContainText("Parry window at 120 ms");
  await expect(working).toContainText("14m");

  const waiting = page.locator('[data-agent-row="claude.m5-mbp"]');
  await expect(waiting).toContainText("Confirm 20-minute quest path");
  await expect(waiting).toContainText("on Lock hub quest path");
  await expect(waiting).toContainText("3h 12m");
  const inboxLink = waiting.locator("[data-agent-inbox-link]");
  await expect(inboxLink).toHaveAttribute(
    "href",
    `${BASE}/inbox?mailbox=needs-you&item=${encodeURIComponent(INBOX_ITEM.id)}`,
  );
  // The roster never answers: no response controls on the page.
  await expect(page.getByRole("button", { name: /Send/ })).toHaveCount(0);

  await expect(
    page.locator('[data-agent-row="release-bot.m5-mbp"]'),
  ).toContainText("Never checked in");
});

test("roster keys move, open and hand off to the Inbox", async ({ page }) => {
  await installAgentsApi(page);
  await page.goto(`${BASE}/agents`);
  await expect(page.locator("[data-agents-roster]")).toBeVisible();

  await page.keyboard.press("?");
  const help = page.getByRole("dialog", { name: "Agents shortcuts" });
  await expect(help).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(help).toHaveCount(0);

  await page.keyboard.press("j");
  await expect(
    page.locator('[data-agent-row="claude.m5-mbp"]'),
  ).toHaveAttribute("aria-current", "true");
  await page.keyboard.press("i");
  await expect(page).toHaveURL(/\/inbox\?mailbox=needs-you&item=/);

  await page.goto(`${BASE}/agents`);
  await expect(page.locator("[data-agents-roster]")).toBeVisible();
  await page.keyboard.press("j");
  await page.keyboard.press("j");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/agents\/codex\.m5-mbp$/);
});

test("agent page shows work, runs and notes, with credentials aside", async ({
  page,
}) => {
  await installAgentsApi(page);
  await page.goto(`${BASE}/agents/codex.m5-mbp`);
  await expect(
    page.getByRole("heading", { name: "codex on m5-mbp" }),
  ).toBeVisible();
  await expect(page.locator("[data-agent-state-label]")).toHaveText("Working");
  const run = page.locator('[data-run-id="run-1"]');
  await expect(run).toContainText("exec-7f3a");
  await expect(run).toContainText("Running");
  await expect(run).toContainText("Tune core combat loop");
  const message = page.locator('[data-agent-activity="message"]');
  await expect(message).toContainText("Input buffer set to 6 frames");
  const via = message.locator("[data-run-attribution]");
  await expect(via).toHaveText("via run exec-7f3a");
  await via.click();
  await expect(page).toHaveURL(/\/agents\/codex\.m5-mbp\?run=run-1#runs$/);
  await expect(run).toHaveClass(/bg-accent-soft/);
  await expect(page.getByText("Exclude on m5-mbp…")).toBeVisible();
});

test("waiting agent page links its ask into the Inbox", async ({ page }) => {
  await installAgentsApi(page);
  await page.goto(`${BASE}/agents/claude.m5-mbp`);
  const ask = page.locator("[data-agent-open-ask]");
  await expect(ask).toContainText("Confirm 20-minute quest path");
  await expect(ask).toContainText("3h 12m");
  await expect(
    ask.getByRole("link", { name: "Answer in Inbox" }),
  ).toHaveAttribute(
    "href",
    `${BASE}/inbox?mailbox=needs-you&item=${encodeURIComponent(INBOX_ITEM.id)}`,
  );
});

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`agents states @ ${viewport.name}`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test("roster, empty, failed and agent page", async ({ page }) => {
      const api = await installAgentsApi(page);
      await page.goto(`${BASE}/agents`);
      await expect(page.locator("[data-agents-roster]")).toBeVisible();
      await expectCleanLayout(page, "roster", {
        scrollPositions: ["top", "bottom"],
      });

      await page.goto(`${BASE}/agents/codex.m5-mbp`);
      await expect(page.locator('[data-run-id="run-1"]')).toBeVisible();
      await expectCleanLayout(page, "agent page", {
        scrollPositions: ["top", "bottom"],
      });

      await page.goto(`${BASE}/agents/nobody.m5-mbp`);
      await expect(
        page.getByText("No agent named nobody.m5-mbp"),
      ).toBeVisible();
      await expectCleanLayout(page, "unknown agent");

      api.roster = [];
      await page.goto(`${BASE}/agents`);
      await expect(page.locator("[data-agents-empty]")).toBeVisible();
      await expectCleanLayout(page, "no agents");

      api.fail.roster = "roster backend unavailable ".repeat(5);
      await page.goto(`${BASE}/agents`);
      await expect(page.getByText("Agents did not load")).toBeVisible();
      await expectCleanLayout(page, "roster failed");
    });
  });
}
