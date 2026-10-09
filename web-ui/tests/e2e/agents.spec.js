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
    host_slug: "workstation-a",
    name,
    handle: `${name}.workstation-a`,
    display_name: `${name} on workstation-a`,
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
const WAITING_ASK = {
  id: "evt-ask",
  inbox_item_id: "inbox:ask:thread-1:evt-ask:evt-ask",
  title: "Confirm 20-minute quest path",
  severity: "high",
  created_at: ago(192),
  kind: "ask",
  subject_ref: "card:lock-hub-quest-path",
  subject_title: "Lock hub quest path",
  requester_actor_id: "actor-claude",
  requester_agent_id: "agent-claude",
};
const OMAR = agent("claude", "waiting_on_human", {
  open_asks_count: 1,
  waiting_ask: WAITING_ASK,
});
const IDLE = agent("reviewer", "idle", { last_signal_at: ago(120) });
/*
 * Core seeds every roster row with `state: "stale"` and only overwrites it
 * with waiting / working / idle (`commandcenter/roster.go`), so every agent
 * silent beyond 24h arrives as "stale". What it means now depends on whether
 * work is riding on the silence.
 */
// Enrolled, never run: roster bookkeeping, folded away.
const NEVER = agent("release-bot", "stale", { last_signal_at: null });
// Silent two days, holding nothing: offline, and not a warning.
const OFFLINE = agent("packager", "stale", { last_signal_at: ago(60 * 60) });
// Silent two days while holding a card: the one that is actually alarming.
const STALE = agent("builder", "stale", {
  last_signal_at: ago(60 * 50),
  current_card_ref: "card:lock-hub-quest-path",
  current_card_title: "Lock hub quest path",
});
const ROSTER = [NEVER, OFFLINE, STALE, IDLE, LEO, OMAR];

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

/**
 * One recent task, as `/agents/{id}` returns them.
 *
 * `minutes` is explicit because core does *not* order these by recency — it
 * orders by board, column and rank — so a fixture that happens to arrive
 * newest-first cannot tell a sort from a slice.
 */
const recentCard = (index, minutes = 30 + index) => ({
  id: `card-uuid-${index}`,
  ref: `card:recent-${index}`,
  handle: `recent-${index}`,
  title: `Recent task ${index}`,
  column_key: "in_progress",
  updated_at: ago(minutes),
});

/**
 * The worst case a row has to survive: a computed status that disagrees with
 * the board, progress, and an ask. `/agents/{id}` computes none of this, so
 * the page resolves the refs it is showing and reads the summary off those.
 */
const LOUD_SUMMARY = {
  status: { state: "blocked", label: "Blocked", reason: "A step is blocked." },
  set_status: { state: "in_progress", label: "In progress" },
  progress: { done: 2, total: 5, unit: "steps" },
  attention: { count: 1, oldest_age: 7200 },
};

async function installAgentsApi(page, overrides = {}) {
  const api = {
    roster: ROSTER,
    fail: {},
    detailCalls: [],
    resolveCalls: [],
    /** Null keeps the single-card default below. */
    recentCards: null,
    /** Summary handed back for every resolved card ref. */
    cardSummary: LOUD_SUMMARY,
    ...overrides,
  };
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
  await mock(/\/agents\/[^/?]+(\?.*)?$/, (route) => {
    const url = new URL(route.request().url());
    const key = decodeURIComponent(url.pathname.split("/agents/")[1]);
    api.detailCalls.push(`${key}?summary=${url.searchParams.get("summary")}`);
    const optedIn = url.searchParams.get("summary") === "1";
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
      /*
       * `summary=1` is what makes core compute these; without it the route
       * answers with the legacy card rows and no summary at all, which is
       * what the page used to work around with its own resolve.
       */
      cards_truncated: api.cardsTruncated || undefined,
      recent_cards: (
        api.recentCards ??
        (found === LEO
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
          : [])
      ).map((card) =>
        optedIn ? { ...card, work_summary: api.cardSummary } : card,
      ),
      recent_runs: found === LEO ? [RUN] : [],
      open_asks: found === OMAR ? [WAITING_ASK] : [],
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
  await mock(/\/refs\/resolve$/, (route) => {
    const refs = route.request().postDataJSON()?.refs ?? [];
    api.resolveCalls.push(refs);
    return json(route, 200, {
      items: refs.map((ref) => ({
        ref,
        resolvable: true,
        kind: "card",
        title: ref,
        summary: api.cardSummary,
      })),
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
        slug: "workstation-a",
        display_name: "workstation-a",
        os_user: "operator",
        hostname: "workstation-a.local",
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
  const api = await installAgentsApi(page);
  await page.goto(`${BASE}/agents`);
  const roster = page.locator("[data-agents-roster]");
  await expect(roster).toBeVisible();
  // "Stale" is reserved for a silence with work riding on it; an agent that
  // is simply not running is offline, and one that never checked in is folded
  // into a counted group rather than listed beside working agents.
  await expect(roster.locator("section > h2")).toHaveText([
    /Waiting on you\s*1/,
    /Working\s*1/,
    /Idle\s*1/,
    /Stale\s*1/,
    /Offline\s*1/,
  ]);
  await expect(page.locator("[data-agent-summary]")).toContainText(
    "1 waiting on you",
  );
  // Five agents that exist in practice; the never-run identity is counted apart.
  await expect(page.locator("[data-agent-summary]")).toContainText("5 agents");
  const folded = page.locator('[data-agents-fold="inactive"]');
  await expect(folded).toContainText("Inactive identities");
  await expect(folded).toContainText("(1)");
  // Folded shut: its rows are not on the first screen.
  await expect(
    page.locator('[data-agent-row="release-bot.workstation-a"]'),
  ).toBeHidden();
  await expect(page.locator('[data-agent-dot="stale"]').first()).toBeVisible();
  await expect(page.locator("[data-agents-nav-count]").first()).toHaveText("1");

  const working = page.locator('[data-agent-row="codex.workstation-a"]');
  await expect(working).toContainText("codex sol");
  await expect(working).toContainText("Parry window at 120 ms");
  await expect(working).toContainText("14m");

  const waiting = page.locator('[data-agent-row="claude.workstation-a"]');
  await expect(waiting).toContainText("Confirm 20-minute quest path");
  await expect(waiting).toContainText("on Lock hub quest path");
  await expect(waiting).toContainText("3h 12m");
  const inboxLink = waiting.locator("[data-agent-inbox-link]");
  await expect(inboxLink).toHaveAttribute(
    "href",
    `${BASE}/inbox?mailbox=needs-you&item=${encodeURIComponent(INBOX_ITEM.id)}`,
  );
  // Waiting rows come from the roster alone: no per-agent detail reads.
  expect(api.detailCalls).toEqual([]);
  // The roster never answers: no response controls on the page.
  await expect(page.getByRole("button", { name: /Send/ })).toHaveCount(0);

  // Silence only reads as an alarm where something is waiting on it.
  await expect(
    page.locator('[data-agent-row="builder.workstation-a"]'),
  ).toContainText("No signal for 2d 2h on this task");
  await expect(
    page.locator('[data-agent-row="packager.workstation-a"]'),
  ).toContainText("Not running");

  await folded.locator("summary").click();
  await expect(
    page.locator('[data-agent-row="release-bot.workstation-a"]'),
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
    page.locator('[data-agent-row="claude.workstation-a"]'),
  ).toHaveAttribute("aria-current", "true");
  await page.keyboard.press("i");
  await expect(page).toHaveURL(/\/inbox\?mailbox=needs-you&item=/);

  await page.goto(`${BASE}/agents`);
  await expect(page.locator("[data-agents-roster]")).toBeVisible();
  await page.keyboard.press("j");
  await page.keyboard.press("j");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/agents\/codex\.workstation-a$/);
});

test("agent page shows work, runs and notes, with credentials aside", async ({
  page,
}) => {
  await installAgentsApi(page);
  await page.goto(`${BASE}/agents/codex.workstation-a`);
  await expect(
    page.getByRole("heading", { name: "codex on workstation-a" }),
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
  await expect(page).toHaveURL(
    /\/agents\/codex\.workstation-a\?run=run-1#runs$/,
  );
  await expect(run).toHaveClass(/bg-accent-soft/);
  await expect(page.getByText("Exclude on workstation-a…")).toBeVisible();
});

test("waiting agent page links its ask into the Inbox", async ({ page }) => {
  await installAgentsApi(page);
  await page.goto(`${BASE}/agents/claude.workstation-a`);
  const ask = page.locator("[data-agent-open-ask]");
  await expect(ask).toContainText("Confirm 20-minute quest path");
  // created_at is an instant: 192 minutes floors to the shared hour phrase.
  await expect(ask).toContainText("3 h ago");
  await expect(
    ask.getByRole("link", { name: "Answer in Inbox" }),
  ).toHaveAttribute(
    "href",
    `${BASE}/inbox?mailbox=needs-you&item=${encodeURIComponent(INBOX_ITEM.id)}`,
  );
});

test("a loud status never squeezes the task link off a phone", async ({
  page,
}) => {
  /*
   * As one flex row the status sat beside the title and refused to shrink,
   * so at 390px "Blocked · marked in progress" with a progress count and an
   * ask left the link zero pixels wide — present in the DOM, invisible, and
   * impossible to click. The summary takes its own line until there is room.
   */
  await page.setViewportSize({ width: 390, height: 844 });
  await installAgentsApi(page);
  await page.goto(`${BASE}/agents/codex.workstation-a`);

  // Scoped to the list: the same task is linked from the runs and activity
  // sections too, and those are not the row that collapsed.
  const row = page.locator("[data-agent-tasks] li").first();
  const link = row.getByRole("link", { name: "Tune core combat loop" });
  await expect(link).toBeVisible();
  const box = await link.boundingBox();
  // A real target, not merely non-zero: the old flex layout left about 90px
  // at the narrowest desktop column, so a low floor proves little.
  expect(box.width).toBeGreaterThan(150);

  // The status is on its own line, under the title rather than beside it.
  const summary = row.locator("[data-work-summary]").first();
  expect((await summary.boundingBox()).y).toBeGreaterThan(box.y);

  // And the link does what a link does.
  await link.click();
  await expect(page).toHaveURL(/\/tasks\/card%3Atune-core-combat-loop$/);
});

test("the task link stays a usable target at every audited width", async ({
  page,
}) => {
  /*
   * 1024 is the tightest: the sidebar appears there, so the content column
   * is narrower than it is at 768 with no sidebar. That is where the title
   * column sits on its floor, and a floor of six rems left about ten
   * characters — clickable, but not a title anybody could read.
   */
  await installAgentsApi(page);
  await page.goto(`${BASE}/agents/codex.workstation-a`);
  const link = page
    .locator("[data-agent-tasks] li")
    .first()
    .getByRole("link", { name: "Tune core combat loop" });
  for (const width of [640, 768, 1024, 1280, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(link).toBeVisible();
    const box = await link.boundingBox();
    expect(box.width, `${width}px`).toBeGreaterThan(150);
  }
});

test("the recent task list and its summary reads are bounded", async ({
  page,
}) => {
  /*
   * `/agents/{id}` returns every card the agent is assigned, with no limit of
   * its own. Rendering all of them was a page nobody scrolls; resolving all
   * of them made the summary work grow with how much the agent had ever been
   * given. Both are the window now, and the window is one batch.
   */
  /*
   * Deliberately *not* newest-first on the wire: core orders by board,
   * column and rank, so the freshest card can arrive anywhere. Card 24 is
   * the one the agent touched a minute ago and it arrives last; card 0 is
   * two months stale and arrives first.
   */
  const api = await installAgentsApi(page, {
    recentCards: Array.from({ length: 25 }, (_, index) =>
      recentCard(index, 25 - index),
    ),
  });
  await page.goto(`${BASE}/agents/codex.workstation-a`);

  const rows = page.locator("[data-agent-tasks] li");
  await expect(rows).toHaveCount(20);
  await expect(page.locator("[data-agent-tasks-capped]")).toContainText(
    "20 most recently updated of 25",
  );
  // The window is the twenty freshest, so the newest leads and the five
  // stalest are the ones cut — not the twenty that happened to arrive first.
  await expect(rows.first()).toContainText("Recent task 24");
  await expect(page.getByText("Recent task 0", { exact: true })).toHaveCount(0);
  await expect(page.locator("[data-agent-tasks]")).toContainText(
    "Recent task 5",
  );

  /*
   * And no second request to make up for the route: `summary=1` carries the
   * computed summary on the rows themselves, from a window core bounds. The
   * page used to resolve every ref it had been handed, which is how an agent
   * with four hundred cards cost three batches.
   */
  expect(api.detailCalls).toEqual(["codex.workstation-a?summary=1"]);
  expect(api.resolveCalls).toEqual([]);
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

      await page.goto(`${BASE}/agents/codex.workstation-a`);
      await expect(page.locator('[data-run-id="run-1"]')).toBeVisible();
      await expectCleanLayout(page, "agent page", {
        scrollPositions: ["top", "bottom"],
      });

      await page.goto(`${BASE}/agents/nobody.workstation-a`);
      await expect(
        page.getByText("No agent named nobody.workstation-a"),
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
