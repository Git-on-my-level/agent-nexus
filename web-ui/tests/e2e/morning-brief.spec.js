import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * The morning brief, on a workspace the size of a real one.
 *
 * The seed is the shape dogfooding found: seven initiatives, twenty-six open
 * asks, fifty cards and twenty agents of which almost none have signalled
 * recently. That is the case the band has to survive — a flat list of
 * twenty-six rows and a roster of twenty mostly-asleep identities is exactly
 * what made the old Overview unreadable.
 *
 * The before/after pair comes from one run: the "before" response omits
 * `brief`, which is precisely how an older core answers, and renders the page
 * as it was. Both captures are reproducible from this file alone.
 */

const OVERVIEW = "/o/local/w/local/overview";
const NOW = "2026-10-08T08:12:00.000Z";
const at = (hours) =>
  new Date(Date.parse(NOW) - hours * 3600_000).toISOString();

const INITIATIVES = [
  {
    title: "Hosted billing",
    state: "blocked",
    reason: "An unfinished step or dependency is blocked.",
    progress: { done: 1, total: 4 },
  },
  {
    title: "Launch the customer pilot",
    state: "at_risk",
    reason: "An open step or initiative is overdue or due within 24 hours.",
    progress: { done: 3, total: 7 },
  },
  {
    title: "Measure daily usefulness",
    state: "stale",
    reason:
      "No card, plan or step activity within the configured stale threshold.",
    progress: { done: 2, total: 6 },
  },
  {
    title: "Ship onboarding",
    state: "no_plan",
    reason: "Initiative has no plan steps.",
    progress: { done: 0, total: 3 },
  },
  {
    title: "Close the feedback loop",
    state: "no_plan",
    reason: "Initiative has no plan steps.",
    progress: { done: 0, total: 3 },
  },
  {
    title: "Improve memory retrieval",
    state: "no_plan",
    reason: "Initiative has no plan steps.",
    progress: { done: 0, total: 3 },
  },
  {
    title: "Prepare the launch story",
    state: "on_track",
    reason: "Open steps are progressing.",
    progress: { done: 4, total: 5 },
  },
];

/** Fifty cards: the seven initiatives, then ordinary work around them. */
const work = [
  ...INITIATIVES.map((initiative, index) => ({
    ref: `card:initiative-${index}`,
    handle: `initiative-${index}`,
    title: initiative.title,
    summary: `A clear outcome for ${initiative.title.toLowerCase()}.`,
    phase: initiative.state === "blocked" ? "blocked" : "in_progress",
    priority: index < 2 ? "p1" : "p2",
    board_ref: "board:demo",
    source: { authority: "nexus" },
    freshness: { status: "unknown" },
    updated_at: at(index + 1),
  })),
  ...Array.from({ length: 43 }, (_, index) => ({
    ref: `card:work-${index}`,
    handle: `work-${index}`,
    title: `Supporting task ${index + 1}`,
    phase:
      index % 9 === 0 ? "blocked" : index % 4 === 0 ? "done" : "in_progress",
    priority: "p3",
    board_ref: "board:work",
    source: { authority: "nexus" },
    freshness: { status: "unknown" },
    updated_at: at(index % 30),
  })),
];

/** Twenty-six open asks, which is what "a flat list of 26 rows" was. */
const asks = Array.from({ length: 26 }, (_, index) => ({
  id: `ask-${index}`,
  title:
    index === 0
      ? "Approve the pricing change"
      : index === 1
        ? "Choose the launch date"
        : `Review the ${index}th adapter contract`,
  kind: "ask",
  status: "open",
  requester_label: "claude",
  created_at: at(index * 6 + 1),
}));

/** Twenty agents: two working, one waiting, one stuck, the rest asleep. */
const agents = Array.from({ length: 20 }, (_, index) => {
  const base = {
    id: `agent-${index}`,
    handle: `agent-${index}`,
    display_name:
      index < 2 ? "claude" : index === 2 ? "codex" : `persona-${index}`,
    last_signal_at: at(index < 4 ? 1 : 24 * (index + 1)),
  };
  if (index < 2) return { ...base, state: "working" };
  if (index === 2) return { ...base, state: "waiting_on_human" };
  if (index === 3)
    return {
      ...base,
      state: "stale",
      current_card_ref: "card:initiative-0",
      current_card_title: "Hosted billing",
      last_signal_at: at(5),
    };
  return { ...base, state: "stale" };
});

const needsRows = [
  ...asks.map((ask) => ({
    id: `inbox:${ask.id}`,
    title: ask.title,
    source: "claude",
    href: `/inbox?mailbox=needs-you&item=inbox%3A${ask.id}`,
  })),
];

/** What core computes. The ranking itself is covered by the Go unit tests. */
const brief = {
  status: "ok",
  generated_at: NOW,
  section_limit: 5,
  throughput_hours: 24,
  decisions: {
    status: "ok",
    count: 26,
    more: 21,
    href: "/inbox?mailbox=needs-you",
    items: [
      {
        id: "inbox:ask-0",
        title: "Approve the pricing change",
        href: "/inbox?mailbox=needs-you&item=inbox%3Aask-0",
        reason: "blocks 2 cards · 1d old",
        signals: { kind: "ask", blocks: 2, age_hours: 25 },
      },
      {
        id: "inbox:ask-1",
        title: "Choose the launch date",
        href: "/inbox?mailbox=needs-you&item=inbox%3Aask-1",
        reason: "overdue 2d · high priority",
        signals: { kind: "ask", blocks: 0 },
      },
      {
        id: "task:card:initiative-0",
        title: "Hosted billing",
        href: "/tasks/initiative-0",
        reason: "blocked · 1d without a change",
        signals: { kind: "task", blocks: 0, phase: "blocked" },
      },
      {
        id: "inbox:ask-2",
        title: "Review the 2th adapter contract",
        href: "/inbox?mailbox=needs-you&item=inbox%3Aask-2",
        reason: "13h old",
        signals: { kind: "ask", blocks: 0 },
      },
      {
        id: "inbox:ask-3",
        title: "Review the 3th adapter contract",
        href: "/inbox?mailbox=needs-you&item=inbox%3Aask-3",
        reason: "19h old",
        signals: { kind: "ask", blocks: 0 },
      },
    ],
  },
  since_last_look: {
    since: at(12),
    total: 9,
    groups: [
      {
        key: "completed",
        label: "Finished",
        count: 4,
        more: 2,
        items: [
          {
            ref: "card:work-4",
            title: "Supporting task 5",
            href: "/tasks/work-4",
            at: at(2),
          },
          {
            ref: "card:work-8",
            title: "Supporting task 9",
            href: "/tasks/work-8",
            at: at(5),
          },
        ],
      },
      {
        key: "newly_blocked",
        label: "Newly blocked",
        count: 1,
        more: 0,
        items: [
          {
            ref: "card:initiative-0",
            title: "Hosted billing",
            href: "/tasks/initiative-0",
            at: at(1),
          },
        ],
      },
      {
        key: "new_asks",
        label: "New asks",
        count: 2,
        more: 0,
        items: [
          {
            ref: "inbox:ask-0",
            title: "Approve the pricing change",
            href: "/inbox?mailbox=needs-you",
            at: at(1),
          },
        ],
      },
      {
        key: "steps",
        label: "Plan steps done",
        count: 2,
        more: 0,
        // Two steps of ONE initiative: they share its ref, which is exactly
        // the shape that used to throw each_key_duplicate on expand.
        items: [
          {
            ref: "card:initiative-1",
            title: "Draft the pilot brief",
            href: "/tasks/initiative-1",
            step_id: "draft",
          },
          {
            ref: "card:initiative-1",
            title: "Review the pilot brief",
            href: "/tasks/initiative-1",
            step_id: "review",
          },
        ],
      },
    ],
  },
  at_risk: {
    status: "ok",
    count: 3,
    more: 0,
    items: INITIATIVES.slice(0, 3).map((initiative, index) => ({
      ref: `card:initiative-${index}`,
      title: initiative.title,
      href: `/tasks/initiative-${index}`,
      state: initiative.state,
      reason: initiative.reason,
      since: at(24 * (index + 2)),
      progress: initiative.progress,
    })),
  },
  machine: {
    status: "ok",
    roster_status: "ok",
    working: 2,
    waiting: 1,
    stuck: 1,
    finished_24h: 6,
    agents_finished_24h: 3,
    href: "/agents",
    stuck_items: [
      {
        ref: "actor:agent-3",
        title: "persona-3",
        href: "/agents/agent-3",
        reason: "Silent 5h: no signal while holding Hosted billing.",
      },
    ],
  },
  initiatives: {
    status: "ok",
    count: 7,
    more: 2,
    href: "/tasks",
    by_state: { blocked: 1, at_risk: 1, stale: 1, on_track: 1, no_plan: 3 },
    items: INITIATIVES.slice(0, 5).map((initiative, index) => ({
      ref: `card:initiative-${index}`,
      title: initiative.title,
      href: `/tasks/initiative-${index}`,
      state: initiative.state,
      reason: initiative.reason,
      progress: initiative.progress,
    })),
  },
};

/**
 * The three bounded lists core computes per initiative: what landed in the last
 * week, what is moving, what is ready next.
 */
const stepDigest = (index) => ({
  window_hours: 168,
  completed: {
    items: [
      {
        id: `done-${index}`,
        title: `Finished step ${index}`,
        ref: `card:step-done-${index}`,
        status: "done",
        at: at(30 + index),
      },
    ],
    more: index === 1 ? 2 : 0,
  },
  current: {
    items: [
      {
        id: `active-${index}`,
        title: `Active step ${index}`,
        status: index === 0 ? "blocked" : "active",
      },
    ],
    more: 0,
  },
  next: {
    items: [
      {
        id: `next-${index}`,
        title: `Next step ${index}`,
        status: "not_started",
      },
    ],
    more: index === 1 ? 3 : 0,
  },
});

async function installOverview(
  page,
  { withBrief = true, briefOverride = null } = {},
) {
  await installWorkspaceApi(page, {
    actors: [{ id: "operator", display_name: "Alex", tags: ["human"] }],
    principals: [{ actor_id: "operator", principal_kind: "human" }],
    boards: [
      {
        board: {
          id: "demo",
          handle: "demo",
          title: "Demo · Initiatives",
          state: "active",
        },
      },
      {
        board: {
          id: "work",
          handle: "work",
          title: "Supporting work",
          state: "active",
        },
      },
    ],
    work,
    agents,
    documents: [],
  });
  const snapshot = () => {
    const payload = {
      generated_at: NOW,
      work: { status: "ok", total: work.length, human_count: 3, items: work },
      initiatives: {
        status: "ok",
        count: INITIATIVES.length,
        items: INITIATIVES.map((initiative, index) => ({
          ref: `card:initiative-${index}`,
          title: initiative.title,
          summary: `A clear outcome for ${initiative.title.toLowerCase()}.`,
          phase: work[index].phase,
          priority: work[index].priority,
          needs: [],
          board_ref: "board:demo",
          updated_at: at(index + 1),
          progress: initiative.progress,
          health: {
            status: initiative.state,
            state: initiative.state,
            reason: initiative.reason,
          },
          plan_health: {
            state: initiative.state,
            reason: initiative.reason,
            since: at(24 * (index + 2)),
          },
          plan_state:
            initiative.state === "no_plan"
              ? null
              : {
                  steps: [],
                  progress: initiative.progress,
                  critical_path: [],
                  next_steps: [`step-${index}`],
                  last_movement_at: at(index + 1),
                },
          plan_step_digest:
            initiative.state === "no_plan" ? null : stepDigest(index),
        })),
      },
      needs_you: {
        status: "ok",
        count: needsRows.length,
        rows: needsRows,
        href: "/inbox?mailbox=needs-you",
      },
      dashboard: {
        status: "ok",
        pinned_ref: null,
        has_more: false,
        reports: [],
      },
      agents: { status: "ok", truncated: false, items: agents },
      since_you_last_looked: {
        since: at(12),
        generated_at: NOW,
        items: [],
        truncated: false,
      },
    };
    // An older core sends no brief at all, which is the "before" state.
    if (withBrief) payload.brief = briefOverride ?? brief;
    return payload;
  };
  await page.route("**/*", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/overview" || path === "/workspace/dashboard") {
      return route.fulfill({ json: snapshot() });
    }
    if (path === "/workspace/dashboard/reports") {
      return route.fulfill({
        json: { status: "ok", pinned_ref: null, has_more: false, reports: [] },
      });
    }
    if (path === "/inbox") return route.fulfill({ json: { items: asks } });
    return route.fallback();
  });
}

test("the brief answers the five questions above the fold", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1200 });
  await installOverview(page);
  await page.goto(OVERVIEW);

  const band = page.locator('[data-overview-section="brief"]');
  await expect(band).toBeVisible();
  // The brief is the first thing on the page: it answers "what should I look
  // at", where everything below it answers "here is everything".
  const sections = await page
    .locator("[data-overview-section]")
    .evaluateAll((nodes) => nodes.map((node) => node.dataset.overviewSection));
  expect(sections[0]).toBe("brief");
  for (const panel of ["decisions", "risk", "changes", "machine"]) {
    await expect(band.locator(`[data-brief-panel="${panel}"]`)).toBeVisible();
  }
  // The initiatives row is not a panel any more: the cards below answer it.
  await expect(band.locator('[data-brief-panel="initiatives"]')).toHaveCount(0);
  // Decisions and At risk come first, because they change what to do next.
  const panels = await band
    .locator("[data-brief-panel]")
    .evaluateAll((nodes) => nodes.map((node) => node.dataset.briefPanel));
  expect(panels).toEqual(["decisions", "risk", "changes", "machine"]);

  // Decisions: the top five of twenty-six, ranked, each with its reason.
  const decisions = band.locator('[data-brief-panel="decisions"]');
  await expect(decisions.locator("[data-brief-decision]")).toHaveCount(5);
  await expect(decisions.locator("[data-brief-decision-count]")).toHaveText(
    "26 waiting",
  );
  await expect(decisions.locator('[data-brief-more="decisions"]')).toHaveText(
    "+21 more",
  );
  await expect(decisions.locator("[data-brief-reason]").first()).toHaveText(
    "blocks 2 cards · 1d old",
  );

  // Since you last looked: counts first, items on request.
  const changes = band.locator('[data-brief-panel="changes"]');
  await expect(changes.locator("[data-brief-change-total]")).toHaveText(
    "9 changes",
  );
  await expect(
    changes.locator('[data-brief-group="completed"] .brief__sub'),
  ).toHaveCount(0);
  await changes.locator('[data-brief-group="completed"] button').click();
  await expect(
    changes.getByRole("link", { name: "Supporting task 5", exact: true }),
  ).toBeVisible();

  // At risk: three rows, each with the reason core computed for it.
  const risk = band.locator('[data-brief-panel="risk"]');
  await expect(risk.locator("[data-brief-risk]")).toHaveCount(3);
  await expect(risk.locator("[data-brief-risk]").first()).toContainText(
    "An unfinished step or dependency is blocked.",
  );

  // Machine: twenty agents become four numbers, and only "stuck" is loud.
  const machine = band.locator('[data-brief-panel="machine"]');
  await expect(machine.locator('[data-brief-stat="working"]')).toContainText(
    "2",
  );
  await expect(machine.locator('[data-brief-stat="finished"]')).toContainText(
    "6",
  );
  await expect(machine.locator('[data-brief-stat="stuck"]')).toContainText("1");
  await expect(machine.locator("[data-brief-stuck]")).toContainText(
    "Hosted billing",
  );

  // Initiatives: the cards themselves, open, directly under the band. Three
  // with no plan is still a sentence, now beside the cards it counts.
  const initiatives = page.locator('[data-overview-section="initiatives"]');
  await expect(
    initiatives.locator('[data-overview-initiative-state="no_plan"]'),
  ).toHaveText("3 No plan");
  await expect(
    initiatives.locator("[data-initiative-tile]").first(),
  ).toHaveAttribute("data-tile-health", "blocked");
  // Each card answers "where is this": what landed, what is moving, what next.
  const first = initiatives.locator("[data-initiative-tile]").first();
  await expect(first.locator('[data-tile-step-group="completed"]')).toHaveText(
    "Recently completed",
  );
  await expect(first.locator('[data-tile-step="done-0"]')).toContainText(
    "Finished step 0",
  );
  await expect(first.locator('[data-tile-step="active-0"]')).toContainText(
    "blocked",
  );
  await expect(first.locator('[data-tile-step="next-0"]')).toContainText(
    "Next step 0",
  );
  const second = initiatives.locator("[data-initiative-tile]").nth(1);
  await expect(second.locator('[data-tile-step-more="next"]')).toHaveText(
    "+3 more",
  );

  await mkdir(".screenshots/review", { recursive: true });
  await page.screenshot({
    path: ".screenshots/review/morning-brief-after.png",
    fullPage: true,
  });
});

test("an empty section says so in one line", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1200 });
  await installOverview(page, {
    briefOverride: {
      ...brief,
      decisions: {
        status: "ok",
        count: 0,
        more: 0,
        items: [],
        href: "/inbox?mailbox=needs-you",
      },
      at_risk: { status: "ok", count: 0, more: 0, items: [] },
      since_last_look: { since: NOW, total: 0, groups: [] },
    },
  });
  await page.goto(OVERVIEW);
  const band = page.locator('[data-overview-section="brief"]');
  await expect(band.locator('[data-brief-empty="decisions"]')).toHaveText(
    "Nothing is waiting on your decision.",
  );
  await expect(band.locator('[data-brief-empty="risk"]')).toHaveText(
    "Nothing is off track.",
  );
  await expect(band.locator('[data-brief-empty="changes"]')).toContainText(
    "Nothing new since",
  );
  /*
   * One line each, not a panel of whitespace. An empty section is an answer,
   * and a short one: a dashboard whose top third says "nothing is waiting" in
   * a big empty box has spent a third of the screen on good news.
   */
  for (const panel of ["decisions", "changes", "risk"]) {
    const height = await band
      .locator(`[data-brief-panel="${panel}"]`)
      .evaluate((node) => node.getBoundingClientRect().height);
    expect(height, `${panel} panel height`).toBeLessThan(64);
  }
});

test("the brief reads on a phone", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await installOverview(page);
  await page.goto(OVERVIEW);
  const band = page.locator('[data-overview-section="brief"]');
  await expect(band).toBeVisible();
  // One column, and nothing overflowing it sideways.
  const overflow = await band.evaluate(
    (node) => node.scrollWidth - node.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);
  await expect(
    band.locator('[data-brief-panel="decisions"] [data-brief-decision]'),
  ).toHaveCount(5);
  /*
   * The initiative cards are open here too, and the three step lists have to
   * stay inside a 390px screen: a label column and one line per step, not a
   * heading and a wrapped paragraph each.
   */
  const lists = page.locator("[data-initiative-tile] [data-tile-steps]");
  await expect(lists.first()).toBeVisible();
  const spill = await lists
    .first()
    .evaluate((node) => node.scrollWidth - node.clientWidth);
  expect(spill).toBeLessThanOrEqual(1);
  await mkdir(".screenshots/review", { recursive: true });
  await page.screenshot({
    path: ".screenshots/review/morning-brief-phone.png",
    fullPage: true,
  });
});

test("a core with no brief renders the page it used to", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1200 });
  await installOverview(page, { withBrief: false });
  await page.goto(OVERVIEW);
  // Backward compatible in the direction that matters: the band is absent
  // rather than empty, and the rest of the Overview is untouched.
  await expect(page.locator('[data-overview-section="brief"]')).toHaveCount(0);
  await expect(page.locator('[data-overview-section="urgent"]')).toBeVisible();
  await expect(
    page.locator('[data-overview-section="initiatives"]'),
  ).toBeVisible();
  await mkdir(".screenshots/review", { recursive: true });
  await page.screenshot({
    path: ".screenshots/review/morning-brief-before.png",
    fullPage: true,
  });
});

/*
Expanding a group whose rows share one ref must not take the page down.

Two completed steps of one initiative carry the initiative's ref, so keyed
rendering on ref alone threw `each_key_duplicate` the moment "Plan steps
done" was opened — in the production build as well as in dev. A unit test on
the key cannot see that; only mounting the list and opening the group can.
*/
test("expanding a group whose rows share a ref renders both rows", async ({
  page,
}) => {
  const crashes = [];
  page.on("pageerror", (error) => crashes.push(String(error)));
  await page.setViewportSize({ width: 1440, height: 1200 });
  await installOverview(page);
  await page.goto(OVERVIEW);

  const group = page.locator(
    '[data-overview-section="brief"] [data-brief-group="steps"]',
  );
  await expect(group.locator("button")).toContainText("Plan steps done");
  await group.locator("button").click();

  await expect(
    group.getByRole("link", { name: "Draft the pilot brief", exact: true }),
  ).toBeVisible();
  await expect(
    group.getByRole("link", { name: "Review the pilot brief", exact: true }),
  ).toBeVisible();
  expect(crashes, "expanding the group threw").toEqual([]);
});

/*
The brief owns this workspace, so the page must not say the same thing twice.
*/
test("the brief replaces the lower sections that repeat it", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1200 });
  await installOverview(page);
  await page.goto(OVERVIEW);

  /*
   * The initiative cards are the primary section after the brief, open on
   * arrival. They used to be folded behind "All initiatives" while the brief
   * carried a line per initiative; the line is gone, so nothing repeats and
   * nothing is hidden. There is no toggle left to click.
   */
  // evaluateAll does not wait; the sections arrive with the snapshot fetch.
  await expect(page.locator("[data-initiative-tile]").first()).toBeVisible();
  const sections = await page
    .locator("[data-overview-section]")
    .evaluateAll((nodes) => nodes.map((node) => node.dataset.overviewSection));
  expect(sections.slice(0, 2)).toEqual(["brief", "initiatives"]);
  await expect(page.locator("[data-initiative-tile]").first()).toBeVisible();
  await expect(page.locator("[data-overview-initiatives-toggle]")).toHaveCount(
    0,
  );

  /*
   * The urgent band listed the same asks the brief now ranks. On this
   * single-workspace fixture it has nothing left that the brief cannot see,
   * so it does not render at all rather than contradicting the brief with
   * "nothing is waiting on you" directly under "26 waiting".
   */
  await expect(page.locator('[data-overview-section="urgent"]')).toHaveCount(0);
});

test("without a brief the page keeps its original sections", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1200 });
  await installOverview(page, { withBrief: false });
  await page.goto(OVERVIEW);
  // The band is the top of the page again, and the tiles are open.
  await expect(page.locator('[data-overview-section="urgent"]')).toBeVisible();
  await expect(page.getByRole("heading", { name: "Needs you" })).toBeVisible();
  await expect(page.locator("[data-initiative-tile]").first()).toBeVisible();
  // No brief, no state counts; the cards still carry their own step lists.
  await expect(page.locator("[data-overview-initiative-state]")).toHaveCount(0);
  await expect(
    page.locator("[data-initiative-tile] [data-tile-steps]").first(),
  ).toBeVisible();
});

test("a reason is never truncated at phone width", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await installOverview(page);
  await page.goto(OVERVIEW);
  const reasons = page.locator(
    '[data-overview-section="brief"] [data-brief-reason]',
  );
  // count() does not wait; the brief arrives with the snapshot fetch.
  await expect(reasons.first()).toBeVisible();
  const count = await reasons.count();
  expect(count).toBeGreaterThan(0);
  for (let i = 0; i < count; i++) {
    const row = reasons.nth(i);
    const clipped = await row.evaluate((node) => ({
      overflowing: node.scrollWidth > node.clientWidth + 1,
      text: node.textContent.trim(),
      title: node.getAttribute("title"),
    }));
    expect(clipped.overflowing, `"${clipped.text}" is clipped`).toBe(false);
    // And the full text is on hover/long-press wherever it does clip.
    expect(clipped.title).toBe(clipped.text);
  }
});
