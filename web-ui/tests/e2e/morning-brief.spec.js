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
        items: [
          {
            ref: "card:initiative-1",
            title: "Draft the pilot brief",
            href: "/tasks/initiative-1",
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
  for (const panel of [
    "decisions",
    "changes",
    "risk",
    "machine",
    "initiatives",
  ]) {
    await expect(band.locator(`[data-brief-panel="${panel}"]`)).toBeVisible();
  }

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

  // Initiatives: three with no plan is a sentence, not three green chips.
  const initiatives = band.locator('[data-brief-panel="initiatives"]');
  await expect(initiatives.locator('[data-brief-state="no_plan"]')).toHaveText(
    "3 No plan",
  );
  await expect(
    initiatives.locator("[data-brief-initiative] [data-health]").first(),
  ).toHaveAttribute("data-health", "blocked");
  const planless = initiatives.locator(
    '[data-brief-initiative="card:initiative-3"]',
  );
  await expect(planless.locator("[data-health]")).toHaveAttribute(
    "data-health",
    "no_plan",
  );
  await expect(planless.locator("[data-brief-progress]")).toHaveText("0/3");

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
