import { expect, test } from "@playwright/test";

import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * One card, every surface that still renders it, one answer.
 *
 * Before `WorkSummary` each surface decided a card's state for itself from a
 * different field of the same response: the Tasks table printed the stored
 * phase, the board card badged `Blocked` off `work.phase`, and the Overview
 * read computed plan health. The same card could read In progress in the
 * table and Blocked on the dashboard at the same time, and a reader had no
 * way to tell which was true.
 *
 * So the assertion is equality, not content: whatever the status and the
 * progress say, every surface has to say the same thing. The card below is
 * deliberately one where the old surfaces disagreed — core computes it as
 * blocked while the board still files it under In progress.
 */

const WORKSPACE = "/o/local/w/local";
const WATCH = `${WORKSPACE}/watch`;
const TASKS = `${WORKSPACE}/initiatives`;
const CARD_REF = "card:release-b";
const NOW = "2026-10-04T12:00:00.000Z";
/** Two hours before NOW, so the age badge is a stable "2h". */
const MOVED_AT = "2026-10-04T10:00:00.000Z";

/** The computed summary core sends on every card-bearing read. */
const workSummary = {
  status: {
    state: "blocked",
    label: "Blocked",
    reason: "A step on the critical path is blocked.",
    since: MOVED_AT,
  },
  // The board still files it under In progress. Core sends the stored phase
  // only because it disagrees, and every surface has to show both.
  set_status: { state: "in_progress", label: "In progress" },
  progress: { done: 2, total: 5, unit: "steps" },
  next: { id: "ship", title: "Ship it", ref: "", more: 1 },
  last_movement_at: MOVED_AT,
  age: 7200,
  attention: {
    count: 10,
    oldest_age: 47 * 86400,
    oldest_at: "2026-08-18T12:00:00Z",
  },
  metrics: [
    {
      ref: "series:cost",
      name: "cost",
      unit: "USD",
      status: "ok",
      latest: { value: 80 },
      baseline: { value: 100 },
      change: -20,
      observed_at: MOVED_AT,
      stale: false,
    },
  ],
};

const card = {
  ref: CARD_REF,
  handle: "release-b",
  title: "Release B",
  summary: "**Goal:** ship the summary primitive.",
  summary_text: "**Goal:** ship the summary primitive.",
  phase: "in_progress",
  column_key: "in_progress",
  priority: "p1",
  board_ref: "board:initiatives",
  updated_at: MOVED_AT,
  source: { authority: "nexus" },
  freshness: { status: "unknown" },
  work_summary: workSummary,
};

/** A second card on a board with no role, so the filter has something to drop. */
const sideCard = {
  ...card,
  ref: "card:side-quest",
  handle: "side-quest",
  title: "Side quest",
  board_ref: "board:other",
  work_summary: {
    status: { state: "on_track", label: "In progress", reason: "Moving." },
    progress: { done: 1, total: 2, unit: "steps" },
    last_movement_at: MOVED_AT,
  },
};

/**
 * A planless card as core computes it once `no_plan` stops being a status:
 * the state comes from the phase, the missing plan is a hint, and the stored
 * phase agrees so there is no `set_status` at all.
 */
const planlessCard = {
  ref: "card:plainwork",
  handle: "plainwork",
  title: "Plain work",
  summary: "A task nobody wrote a plan for.",
  summary_text: "A task nobody wrote a plan for.",
  phase: "in_progress",
  column_key: "in_progress",
  board_ref: "board:initiatives",
  updated_at: MOVED_AT,
  source: { authority: "nexus" },
  freshness: { status: "unknown" },
  work_summary: {
    status: {
      state: "in_progress",
      label: "In progress",
      reason: "The card is being worked.",
    },
    hints: ["no_plan"],
    last_movement_at: MOVED_AT,
  },
};

/**
 * An active dark card whose every linked source has already finished: core
 * reads the state dark (nothing dated will surface it) and flags it possibly
 * moot (the PRs merged under it). The flag is a top-level row field beside
 * the summary, and every surface that renders the card carries the evidence.
 */
const darkCard = {
  ref: "card:quiet-decision",
  handle: "quiet-decision",
  title: "Quiet decision",
  summary: "Active work nothing will surface for weeks.",
  summary_text: "Active work nothing will surface for weeks.",
  phase: "in_progress",
  column_key: "in_progress",
  board_ref: "board:initiatives",
  updated_at: MOVED_AT,
  source: { authority: "nexus" },
  freshness: { status: "unknown" },
  possibly_moot: {
    flagged: true,
    reason: "all 2 linked sources are merged",
    sources: 2,
  },
  work_summary: {
    status: {
      state: "dark",
      label: "Dark",
      reason: "Next dated signal is 2026-11-05, 22 days out.",
      since: "2026-11-05T00:00:00.000Z",
    },
    last_movement_at: MOVED_AT,
  },
};

const boards = [
  {
    board: {
      id: "initiatives",
      handle: "initiatives",
      ref: "board:initiatives",
      title: "Initiatives",
      role: "initiatives",
      state: "active",
    },
  },
  {
    board: {
      id: "other",
      handle: "other",
      ref: "board:other",
      title: "Other work",
      role: "",
      state: "active",
    },
  },
];

/**
 * The response core actually sends, for the parameter the client sent.
 *
 * Core computes `work_summary` only for `?summary=1`, and when it does it
 * moves the prose to `summary_text` and puts the object at `summary`. The
 * mock has to do the same or the spec passes on a shape production never
 * returns — which is how the first draft of this change shipped a Tasks
 * table that silently rendered the legacy fields.
 */
function asCore(row, optedIn) {
  if (!row.work_summary) return row;
  if (!optedIn) {
    const { work_summary, summary_text, ...legacy } = row;
    void work_summary;
    void summary_text;
    return legacy;
  }
  return { ...row, summary: row.work_summary };
}

async function install(page) {
  await page.clock.setFixedTime(new Date(NOW));
  await installWorkspaceApi(page, {
    actors: [{ id: "operator", display_name: "Alex", tags: ["human"] }],
    principals: [{ actor_id: "operator", principal_kind: "human" }],
    work: [card, sideCard, planlessCard, darkCard],
  });
  await page.route("**/*", async (route) => {
    // Decoded: a card ref carries a colon, so the path arrives percent-encoded.
    const url = new URL(route.request().url());
    const path = decodeURIComponent(url.pathname);
    const optedIn = url.searchParams.get("summary") === "1";
    if (path === "/boards") return route.fulfill({ json: { boards } });
    if (path === "/work")
      return route.fulfill({
        json: {
          work: [card, sideCard, planlessCard, darkCard].map((row) =>
            asCore(row, optedIn),
          ),
        },
      });
    if (path === `/work/${CARD_REF}`)
      return route.fulfill({ json: { work: asCore(card, optedIn) } });
    if (path === `/work/${planlessCard.ref}`)
      return route.fulfill({ json: { work: asCore(planlessCard, optedIn) } });
    if (path.startsWith(`/work/${planlessCard.ref}/`))
      return route.fulfill({ json: { observations: [], participants: [] } });
    if (path === `/work/${CARD_REF}/observations`)
      return route.fulfill({ json: { observations: [] } });
    if (path === `/work/${CARD_REF}/participants`)
      return route.fulfill({ json: { participants: [] } });
    if (path.startsWith("/cards/") && path.endsWith("/plan"))
      // No plan read: the header must stand on the summary the card row
      // carried, which is what a card with no plan looks like.
      return route.fulfill({
        status: 404,
        json: { error: { code: "not_found", message: "no plan" } },
      });
    return route.fallback();
  });
}

/**
 * What a surface says about the card, as the reader reads it.
 *
 * `:visible`, because the table carries the same summary twice in one DOM:
 * the Status column above 640px and the two-line row below it. Reading the
 * hidden one would pass a test the reader cannot see.
 */
async function readSummary(scope) {
  const summary = scope.locator("[data-work-summary]:visible").first();
  await expect(summary).toBeVisible({ timeout: 60_000 });
  /*
   * Optional parts read as "" rather than hanging: core omits a part it did
   * not compute, so "absent" is one of the answers a surface can give and
   * the surfaces still have to agree on it.
   */
  const optional = async (selector) => {
    const part = summary.locator(selector).first();
    return (await part.count()) ? (await part.innerText()).trim() : "";
  };
  return {
    status: (await summary.locator("[data-health]").first().innerText()).trim(),
    state: await summary
      .locator("[data-health]")
      .first()
      .getAttribute("data-health"),
    setStatus: await optional("[data-summary-set-status]"),
    progress: await optional("[data-summary-progress]"),
  };
}

for (const viewport of [
  { label: "desktop", width: 1440, height: 1100 },
  { label: "phone", width: 390, height: 844 },
]) {
  test(`one card reads the same on the Initiatives table and board @ ${viewport.label}`, async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await install(page);
    await page.setViewportSize({
      width: viewport.width,
      height: viewport.height,
    });

    await page.goto(`${TASKS}?view=table`);
    const table = await readSummary(
      page.locator(`[data-work-ref="${CARD_REF}"]`),
    );

    await page.goto(`${TASKS}?view=board`);
    const board = await readSummary(
      page.locator(`[data-work-slot][data-work-ref="${CARD_REF}"]`),
    );

    await expect(
      page.locator(
        `[data-work-slot][data-work-ref="${CARD_REF}"] [data-summary-attention]`,
      ),
    ).toContainText("waiting on you 47d");
    await expect(
      page.locator(
        `[data-work-slot][data-work-ref="${CARD_REF}"] [data-summary-metric]`,
      ),
    ).toContainText("vs 100");

    // The point of the whole change.
    expect(board).toEqual(table);

    /*
     * And what they agree on is the computed state plus the board's own
     * disagreement with it, not the stored phase on its own.
     */
    expect(table.state).toBe("blocked");
    expect(table.status).toContain("Blocked");
    expect(table.setStatus).toContain("marked in progress");
    expect(table.progress).toBe("2/5");
  });
}

test("the surfaces ask core to compute the summary", async ({ page }) => {
  /*
   * The mock answers the way core does: no `summary=1`, no computed parts.
   * So if a surface forgets the parameter it falls back to the legacy fields
   * and still renders — which is exactly why this has to be asserted rather
   * than assumed.
   */
  test.setTimeout(120_000);
  await install(page);
  await page.setViewportSize({ width: 1440, height: 1100 });

  const asked = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    // The card reads themselves, not their sub-resources: observations and
    // participants carry no card state and should not pay for a summary.
    if (/^\/work$|^\/work\/[^/]+$/.test(url.pathname)) {
      asked.push(`${url.pathname}?summary=${url.searchParams.get("summary")}`);
    }
  });

  await page.goto(`${TASKS}?view=table`);
  await expect(page.locator(`[data-work-ref="${CARD_REF}"]`)).toBeVisible({
    timeout: 60_000,
  });

  await page.goto(`${TASKS}?view=board`);
  await expect(
    page.locator(`[data-work-slot][data-work-ref="${CARD_REF}"]`),
  ).toBeVisible({ timeout: 60_000 });

  expect(asked.length).toBeGreaterThan(0);
  expect(asked.filter((entry) => !entry.endsWith("?summary=1"))).toEqual([]);
});

test("a planless card reads as its phase in the table and the board, with no plan note in a list", async ({
  page,
}) => {
  /*
   * `no_plan` is not a status. It was one, and the Tasks table's Status
   * column then read "No plan" for most of the rows in it. Core computes the
   * state from the phase now and reports the missing plan separately, so the
   * note belongs where a plan is expected — a card, a page header — and
   * nowhere in a list.
   */
  test.setTimeout(120_000);
  await install(page);
  await page.setViewportSize({ width: 1440, height: 1100 });

  // In the Tasks table: the state is the phase, and there is no note.
  await page.goto(`${TASKS}?view=table`);
  const row = page.locator(`[data-work-ref="${planlessCard.ref}"]`);
  await expect(row).toBeVisible({ timeout: 60_000 });
  const card = await readSummary(row);
  expect(card.state).toBe("in_progress");
  expect(card.status).toContain("In progress");
  // The stored phase agrees, so core sends none and the card claims none.
  expect(card.setStatus).toBe("");
  await expect(row.locator("[data-summary-hints]")).toHaveCount(0);
  await expect(row).not.toContainText("No plan");

  // The board says the same thing about the same card.
  await page.goto(`${TASKS}?view=board`);
  expect(
    await readSummary(
      page.locator(`[data-work-slot][data-work-ref="${planlessCard.ref}"]`),
    ),
  ).toEqual(card);
});

test("the Initiatives filter keeps the same rows, fewer of them", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await install(page);
  await page.setViewportSize({ width: 1440, height: 1100 });

  await page.goto(`${TASKS}?view=table`);
  await expect(page.locator("[data-work-ref]")).toHaveCount(4, {
    timeout: 60_000,
  });

  // It is a filter, not a second renderer: the same rows, fewer of them.
  await page.goto(`${TASKS}?view=table&initiatives=1`);
  await expect(page.locator("[data-task-initiatives-filter]")).toContainText(
    "Showing initiatives only",
  );
  await expect(page.locator("[data-work-ref]")).toHaveCount(3);
  await expect(page.locator(`[data-work-ref="${CARD_REF}"]`)).toBeVisible();
  const filtered = await readSummary(
    page.locator(`[data-work-ref="${CARD_REF}"]`),
  );
  expect(filtered.state).toBe("blocked");
});

test("a dark card shows the state and the possibly-moot evidence everywhere", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await install(page);
  await page.setViewportSize({ width: 1440, height: 1100 });

  // The Tasks row: dark chip, and the moot flag beside it with its evidence.
  await page.goto(`${TASKS}?view=table`);
  const row = page.locator(`[data-work-ref="${darkCard.ref}"]`);
  await expect(row).toBeVisible({ timeout: 60_000 });
  const summary = row.locator("[data-work-summary]:visible").first();
  await expect(summary.locator("[data-health]").first()).toHaveAttribute(
    "data-health",
    "dark",
  );
  await expect(summary.locator("[data-moot]")).toContainText("Possibly moot");
});

test("one initiative reads the same on the Watch list, the Watch board and its page", async ({
  page,
}) => {
  /*
   * The surfaces that render an initiative's computed state now all read the
   * brief. Whatever the state and the promise line say, the list row, the
   * by-stage card and the initiative page header have to say the same thing.
   */
  test.setTimeout(120_000);
  const ref = "card:battery-measurement";
  const row = brief.initiatives.find((item) => item.ref === ref);
  await page.setViewportSize({ width: 1440, height: 1100 });
  await installWorkspaceApi(page, {
    actors: [{ id: "operator", display_name: "Alex", tags: ["human"] }],
    principals: [{ actor_id: "operator", principal_kind: "human" }],
    work: [
      {
        ref,
        handle: "battery-measurement",
        title: row.title,
        summary: "Measure the battery.",
        phase: "in_progress",
        column_key: "in_progress",
        source: { authority: "nexus" },
        freshness: { status: "unknown" },
      },
    ],
  });
  const workRow = {
    ref,
    handle: "battery-measurement",
    title: row.title,
    summary: "Measure the battery.",
    phase: "in_progress",
    column_key: "in_progress",
    source: { authority: "nexus" },
    freshness: { status: "unknown" },
  };
  await page.route(
    (url) =>
      [`/work/${ref}`, "/work/battery-measurement"].includes(
        decodeURIComponent(url.pathname),
      ),
    (route) => route.fulfill({ json: { work: workRow } }),
  );
  await page.route(
    (url) => url.pathname === "/brief",
    (route) =>
      route.fulfill({
        json: { ...brief, generated_at: new Date().toISOString() },
      }),
  );

  const LABELS = {
    past_promise: "Past promise",
    moving: "Moving",
    no_promise: "No promise",
    done: "Done",
  };
  const read = async (scope) => {
    // The list groups by state and the row carries it as an attribute; the
    // by-stage card and the page header print the label.
    const stateAttr = await scope
      .getAttribute("data-watch-state")
      .catch(() => null);
    const state = stateAttr
      ? LABELS[stateAttr]
      : (
          await scope
            .locator('[data-source="computed:initiatives.state"]')
            .first()
            .innerText()
        ).trim();
    const parts = {};
    for (const field of ["promise", "past_promise_for"]) {
      const part = scope
        .locator(`[data-source="computed:initiatives.${field}"]`)
        .first();
      parts[field] = (await part.count())
        ? (await part.innerText()).trim()
        : "";
    }
    return { state, parts };
  };

  await page.goto(WATCH);
  const list = await read(page.locator(`[data-watch-row="${ref}"]`));
  await page.goto(`${WATCH}?view=board`);
  const board = await read(
    page.locator(`[data-watch-row="${ref}"][data-watch-variant="card"]`),
  );
  // The card links to the initiative page, which is the drill-down.
  const href = await page
    .locator(`[data-watch-row="${ref}"] [data-watch-title]`)
    .getAttribute("href");
  await page.goto(href);
  const header = await read(page.locator("main").first());

  expect(list.state).toBe("Past promise");
  expect(list.parts.past_promise_for).toContain("past");
  expect(board).toEqual(list);
  expect(header).toEqual(list);
});
