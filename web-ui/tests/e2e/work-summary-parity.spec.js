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

async function install(page) {
  await installWorkspaceApi(page, {
    actors: [{ id: "operator", display_name: "Alex", tags: ["human"] }],
    principals: [{ actor_id: "operator", principal_kind: "human" }],
    work: [card, sideCard],
  });
  await page.route("**/*", async (route) => {
    // Decoded: a card ref carries a colon, so the path arrives percent-encoded.
    const path = decodeURIComponent(new URL(route.request().url()).pathname);
    if (path === "/boards") return route.fulfill({ json: { boards } });
    if (path === `/work/${CARD_REF}`)
      return route.fulfill({ json: { work: card } });
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
          work: { status: "ok", total: 2, human_count: 0, items: [card] },
          initiatives: {
            status: "ok",
            count: 1,
            items: [{ ...card, href: `/tasks/${card.handle}` }],
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
  return {
    status: (await summary.locator("[data-health]").first().innerText()).trim(),
    state: await summary
      .locator("[data-health]")
      .first()
      .getAttribute("data-health"),
    setStatus: (
      await summary.locator("[data-summary-set-status]").first().innerText()
    ).trim(),
    progress: (
      await summary.locator("[data-summary-progress]").first().innerText()
    ).trim(),
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

test("the Initiatives filter reuses the board role Overview selects by", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await install(page);
  await page.setViewportSize({ width: 1440, height: 1100 });

  await page.goto(`${TASKS}?view=table`);
  await expect(page.locator("[data-work-ref]")).toHaveCount(2, {
    timeout: 60_000,
  });

  // It is a filter, not a second renderer: the same rows, fewer of them.
  await page.goto(`${TASKS}?view=table&initiatives=1`);
  await expect(page.locator("[data-task-initiatives-filter]")).toContainText(
    "initiatives board",
  );
  await expect(page.locator("[data-work-ref]")).toHaveCount(1);
  await expect(page.locator(`[data-work-ref="${CARD_REF}"]`)).toBeVisible();
  const filtered = await readSummary(
    page.locator(`[data-work-ref="${CARD_REF}"]`),
  );
  expect(filtered.state).toBe("blocked");
});
