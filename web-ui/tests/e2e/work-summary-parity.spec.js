import { expect, test } from "@playwright/test";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * One card, four surfaces, one answer.
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
const OVERVIEW = `${WORKSPACE}/overview`;
const TASKS = `${WORKSPACE}/tasks`;
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
 * A card from a core that computes no summary at all, carrying only the
 * legacy fields — plus a source status Nexus has no phase for. Every surface
 * has to keep saying something about it.
 */
const legacyCard = {
  ref: "card:legacy",
  handle: "legacy",
  title: "Legacy card",
  summary: "A card from a core before work_summary.",
  phase: "in_progress",
  column_key: "in_progress",
  board_ref: "board:other",
  updated_at: MOVED_AT,
  source: {
    authority: "jira",
    connection_id: "jira-main",
    native_id: "OPS-12",
    native_status: "In UAT",
  },
  freshness: { status: "unknown" },
  plan_health: { state: "blocked", reason: "A dependency is blocked." },
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
  await installWorkspaceApi(page, {
    actors: [{ id: "operator", display_name: "Alex", tags: ["human"] }],
    principals: [{ actor_id: "operator", principal_kind: "human" }],
    work: [card, sideCard, legacyCard, planlessCard],
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
          work: [card, sideCard, legacyCard, planlessCard].map((row) =>
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
    if (path === `/work/${legacyCard.ref}`)
      return route.fulfill({ json: { work: legacyCard } });
    if (path.startsWith(`/work/${legacyCard.ref}/`))
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
    if (path === "/overview")
      return route.fulfill({
        json: {
          generated_at: NOW,
          work: {
            status: "ok",
            total: 2,
            human_count: 0,
            items: [asCore(card, optedIn)],
          },
          initiatives: {
            status: "ok",
            count: 2,
            items: [card, planlessCard].map((row) => ({
              ...asCore(row, optedIn),
              href: `/tasks/${row.handle}`,
            })),
          },
          needs_you: { status: "ok", count: 0, rows: [], href: "/inbox" },
          dashboard: { status: "ok", has_more: false, reports: [] },
          agents: { status: "ok", items: [] },
        },
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
  test(`one card reads the same on Overview, table and board @ ${viewport.label}`, async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await install(page);
    await page.setViewportSize({
      width: viewport.width,
      height: viewport.height,
    });

    await page.goto(OVERVIEW);
    const overview = await readSummary(
      page.locator(`[data-initiative-tile="${CARD_REF}"]`),
    );

    await page.goto(`${TASKS}?view=table`);
    const table = await readSummary(
      page.locator(`[data-work-ref="${CARD_REF}"]`),
    );

    await page.goto(`${TASKS}?view=board`);
    const board = await readSummary(
      page.locator(`[data-work-slot][data-work-ref="${CARD_REF}"]`),
    );

    // The point of the whole change.
    expect(table).toEqual(overview);
    expect(board).toEqual(overview);

    /*
     * And what they agree on is the computed state plus the board's own
     * disagreement with it, not the stored phase on its own.
     */
    expect(overview.state).toBe("blocked");
    expect(overview.status).toContain("Blocked");
    expect(overview.setStatus).toContain("marked in progress");
    expect(overview.progress).toBe("2/5");
  });
}

test("the task header agrees with the row it was opened from", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await install(page);
  await page.setViewportSize({ width: 1440, height: 1100 });

  await page.goto(`${TASKS}?view=table`);
  const row = await readSummary(page.locator(`[data-work-ref="${CARD_REF}"]`));

  await page.goto(`${TASKS}/${encodeURIComponent(CARD_REF)}`);
  const header = page.locator('[data-work-summary="header"]');
  await expect(header).toBeVisible({ timeout: 60_000 });
  // Header density has room for words, so progress carries its unit; the
  // state, the label and the disagreement are the same facts as the row's.
  await expect(header.locator("[data-health]").first()).toHaveAttribute(
    "data-health",
    row.state,
  );
  await expect(header.locator("[data-health]").first()).toContainText(
    "Blocked",
  );
  await expect(header.locator("[data-summary-set-status]")).toContainText(
    "marked in progress",
  );
  await expect(header.locator("[data-summary-progress]")).toHaveText(
    "2/5 steps",
  );
  // The computed reason is on screen here, not only in the tooltip: a page
  // has room for the sentence, and "Blocked" without it sends the reader
  // looking for why.
  await expect(header.locator("[data-summary-reason]")).toContainText(
    "critical path is blocked",
  );
});

test("a card from a core with no computed summary still reads a state", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await install(page);
  await page.setViewportSize({ width: 1440, height: 1100 });

  await page.goto(`${TASKS}?view=table`);
  const row = page.locator('[data-work-ref="card:legacy"]');
  const legacy = await readSummary(row);
  /*
   * The back-compatibility reader: `plan_health` is the old spelling of the
   * computed state, and the Status column keeps a value rather than going
   * blank on a core that has not shipped `work_summary` yet.
   */
  expect(legacy.state).toBe("blocked");
  expect(legacy.status).toContain("Blocked");
  /*
   * A legacy core computed a health but never said whether the stored phase
   * agreed with it, so the client compares the two itself — which is the only
   * place it decides anything about state, and only when core gave it a
   * computed health to compare against.
   */
  expect(legacy.setStatus).toContain("marked in progress");
  // And it has no plan, so there is no progress to state — an omitted part
  // stays omitted rather than becoming an invented 0/0.
  expect(legacy.progress).toBe("");
  // Same card, same answer, on the board.
  await page.goto(`${TASKS}?view=board`);
  const board = await readSummary(
    page.locator('[data-work-slot][data-work-ref="card:legacy"]'),
  );
  expect(board).toEqual(legacy);

  /*
   * And the tracker's own word for where it stands is back on the page
   * header, which is where it was before this change and where there is room
   * for it. A list row does not carry it: the badge cannot wrap by design,
   * and a fourth one in the Status cell made every row two lines tall.
   */
  await page.goto(`${TASKS}/${encodeURIComponent("card:legacy")}`);
  const header = page.locator('[data-work-summary="header"]');
  await expect(header).toBeVisible({ timeout: 60_000 });
  await expect(header.locator("[data-summary-source-status]")).toHaveText(
    "In UAT",
  );
});

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
    if (/^\/overview$|^\/work$|^\/work\/[^/]+$/.test(url.pathname)) {
      asked.push(`${url.pathname}?summary=${url.searchParams.get("summary")}`);
    }
  });

  await page.goto(OVERVIEW);
  await expect(
    page.locator(`[data-initiative-tile="${CARD_REF}"]`),
  ).toBeVisible({ timeout: 60_000 });
  await page.goto(`${TASKS}?view=table`);
  await expect(page.locator(`[data-work-ref="${CARD_REF}"]`)).toBeVisible({
    timeout: 60_000,
  });
  await page.goto(`${TASKS}/${encodeURIComponent(CARD_REF)}`);
  await expect(page.locator('[data-work-summary="header"]')).toBeVisible({
    timeout: 60_000,
  });

  expect(asked.length).toBeGreaterThan(0);
  expect(asked.filter((entry) => !entry.endsWith("?summary=1"))).toEqual([]);
});

test("a planless card reads as its phase, with the missing plan as a note", async ({
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

  // On the Overview card: the state, and the note under it.
  await page.goto(OVERVIEW);
  /*
   * In the "No plan" fold, found by the hint rather than by a status. That
   * fold is the reason the grouping had to move: core no longer calls a
   * planless card `no_plan`, so reading the status would have folded nothing
   * and dropped every planless initiative into the attention grid.
   */
  const fold = page.locator("[data-initiative-group='no-plan']");
  await expect(fold).toBeVisible({ timeout: 60_000 });
  await fold.locator("summary").click();
  const tile = page.locator(`[data-initiative-tile="${planlessCard.ref}"]`);
  await expect(tile).toBeVisible();
  const card = await readSummary(tile);
  expect(card.state).toBe("in_progress");
  expect(card.status).toContain("In progress");
  // The stored phase agrees, so core sends none and the card claims none.
  expect(card.setStatus).toBe("");
  await expect(tile.locator("[data-summary-hint='no_plan']")).toHaveText(
    "No plan",
  );

  // In the Tasks table: the same state, and no note.
  await page.goto(`${TASKS}?view=table`);
  const row = page.locator(`[data-work-ref="${planlessCard.ref}"]`);
  await expect(row).toBeVisible({ timeout: 60_000 });
  expect(await readSummary(row)).toEqual(card);
  await expect(row.locator("[data-summary-hints]")).toHaveCount(0);
  await expect(row).not.toContainText("No plan");

  // On the task header: the note is back, beside the computed reason.
  await page.goto(`${TASKS}/${encodeURIComponent(planlessCard.ref)}`);
  const header = page.locator('[data-work-summary="header"]');
  await expect(header).toBeVisible({ timeout: 60_000 });
  await expect(header.locator("[data-summary-hint='no_plan']")).toHaveText(
    "No plan",
  );
  await expect(header.locator("[data-summary-set-status]")).toHaveCount(0);
});

test("the Initiatives filter reuses the board role Overview selects by", async ({
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
    "initiatives board",
  );
  await expect(page.locator("[data-work-ref]")).toHaveCount(2);
  await expect(page.locator(`[data-work-ref="${CARD_REF}"]`)).toBeVisible();
  const filtered = await readSummary(
    page.locator(`[data-work-ref="${CARD_REF}"]`),
  );
  expect(filtered.state).toBe("blocked");
});
