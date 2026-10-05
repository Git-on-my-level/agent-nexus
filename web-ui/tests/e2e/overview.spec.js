import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { deferred, installWorkspaceApi } from "../helpers/workspaceApiMock.js";

const OVERVIEW = "/o/local/w/local/overview";
const BEFORE = process.env.OVERVIEW_CAPTURE_BEFORE === "1";
const NOW = "2026-10-04T12:00:00.000Z";
const report = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Demo dashboard",
  summary:
    "Seven initiatives. Two decisions need Alex. Launch readiness is reported by the team.",
  generated_at: NOW,
  projects: [
    {
      id: "demo",
      title: "Demo",
      summary: "Launch preparation",
      outcome: "A useful launch with evidence",
    },
  ],
  sources: [],
  panels: [
    {
      id: "status",
      project_id: "demo",
      type: "explanation",
      title: "Launch readiness",
      author: "claude",
      provenance: "reported",
      observed_at: null,
      freshness: "unknown",
      source_ids: [],
      data: {
        text: "3 initiatives are ready for review. 4 are in progress. Choose the launch date and approve the customer pilot.",
      },
    },
  ],
};
const titles = [
  "Launch the customer pilot",
  "Improve memory retrieval",
  "Ship onboarding",
  "Make voice capture reliable",
  "Measure daily usefulness",
  "Prepare the launch story",
  "Close the feedback loop",
];
const active = titles.map((title, index) => ({
  ref: `card:initiative-${index}`,
  handle: `initiative-${index}`,
  title,
  summary: `A clear outcome for initiative ${index + 1}.\n- [x] Define success\n- [x] Design\n- [x] Review\n- [ ] Implement\n- [ ] Verify\n- [ ] Pilot\n- [ ] Launch\n${index === 0 ? "Needs Alex: approve the pilot" : ""}`,
  phase: "in_progress",
  priority: index < 2 ? "p1" : "p2",
  source: { authority: "nexus" },
  freshness: { status: "unknown" },
}));
const archived = Array.from({ length: 18 }, (_, index) => ({
  ref: `card:old-${index}`,
  title: `Archived backlog ${index + 1}`,
  phase: "backlog",
  source: { authority: "nexus" },
  freshness: { status: "unknown" },
  board_ref: "board:old",
}));
/**
 * Health per seeded initiative, worst first once sorted: one blocked, one at
 * risk, one stale, three on track, one done — plus the planless tail the
 * archived rows provide.
 */
const INITIATIVE_HEALTH = [
  { state: "blocked", reason: "Waiting on the pilot decision." },
  { state: "at_risk", reason: "Two steps slipped their due date." },
  { state: "stale", reason: "Nothing has moved for 9 days." },
  { state: "on_track", reason: "Work is progressing." },
  { state: "on_track", reason: "Work is progressing." },
  { state: "on_track", reason: "Work is progressing." },
  { state: "done", reason: "Every step is done." },
];

const asks = ["Choose the launch date", "Approve the customer pilot"].map(
  (title, index) => ({
    id: `ask-${index}`,
    title,
    kind: "ask",
    status: "open",
    requester_label: "claude",
    created_at: NOW,
  }),
);
const dashboard = {
  document: {
    id: "demo-dashboard",
    handle: "demo-dashboard",
    ref: "document:demo-dashboard",
    title: "Dashboard · Demo",
    state: "active",
    updated_at: NOW,
  },
  revision: { content: JSON.stringify(report), content_type: "text" },
};

async function installOverview(page, { gate = null, failure = false } = {}) {
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
      { board: { id: "old", title: "Old backlog", state: "archived" } },
    ],
    work: [...active, ...archived],
    agents: Array.from({ length: 3 }, (_, index) => ({
      id: `agent-${index}`,
      display_name: "claude",
      state: "stale",
    })),
    documents: [dashboard.document],
  });
  const state = { pinned: null, writes: [], selectorReads: 0 };
  const snapshot = () => ({
    generated_at: NOW,
    work: { status: "ok", total: active.length, human_count: 0, items: active },
    initiatives: {
      status: "ok",
      count: 7,
      items: active.map((item, index) => ({
        ...item,
        // The body is markdown; the tile must show prose, not the source.
        summary: item.summary.split("\n")[0],
        progress: { done: 3, total: 7 },
        // One of each state the attention sort cares about, so the seeded
        // Overview shows the real order rather than seven identical tiles.
        plan_health: INITIATIVE_HEALTH[index],
        plan_state: {
          steps: [],
          progress: { done: 3, total: 7 },
          critical_path: [],
          next_steps: [`step-${index}`],
          last_movement_at: NOW,
        },
        needs:
          item.ref === active[0].ref ? ["Needs Alex: approve the pilot"] : [],
        href: `/tasks/${item.handle}`,
      })),
    },
    needs_you: {
      status: "ok",
      count: 2,
      rows: asks.map((ask) => ({
        id: `inbox:${ask.id}`,
        title: ask.title,
        source: "claude",
        href: `/inbox?mailbox=needs-you&item=inbox%3A${ask.id}`,
      })),
      href: "/inbox?mailbox=needs-you",
    },
    dashboard: {
      status: "ok",
      pinned_ref: state.pinned,
      has_more: true,
      reports: [{ ...dashboard.document, segment: "demo-dashboard", report }],
    },
    agents: {
      status: "ok",
      items: Array.from({ length: 3 }, (_, index) => ({
        id: `agent-${index}`,
        state: "stale",
      })),
    },
  });
  await page.route("**/*", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/overview" || path === "/workspace/dashboard") {
      if (gate) await gate.promise;
      if (failure)
        return route.fulfill({
          status: 503,
          json: {
            error: {
              code: "unavailable",
              message: "Overview could not be loaded",
            },
          },
        });
      if (request.method() === "PUT") {
        state.writes.push(request.postDataJSON());
        state.pinned = request.postDataJSON().document_ref;
      }
      return route.fulfill({ json: snapshot() });
    }
    if (path === "/workspace/dashboard/reports") {
      state.selectorReads++;
      return route.fulfill({
        json: {
          ...snapshot().dashboard,
          has_more: false,
          reports: [
            snapshot().dashboard.reports[0],
            {
              ...snapshot().dashboard.reports[0],
              id: "older-dashboard",
              ref: "document:older-dashboard",
              segment: "older-dashboard",
              title: "Earlier dashboard",
              report: { ...report, title: "Earlier dashboard" },
            },
          ],
        },
      });
    }
    if (path === "/inbox") return route.fulfill({ json: { items: asks } });
    if (path === "/docs/demo-dashboard")
      return route.fulfill({ json: dashboard });
    return route.fallback();
  });
  return state;
}

test("seeded CEO Overview screenshot and section order", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1700 });
  await installOverview(page);
  await page.goto(OVERVIEW);
  // One urgent band at the top: it counts what is waiting and links each row
  // to the surface that owns it, rather than becoming a second Inbox.
  const band = page.locator('[data-overview-section="urgent"]');
  await expect(band).toHaveAttribute("data-urgent-state", "active");
  await expect(band.locator("[data-urgent-ask-count]")).toHaveText("2 asks");
  await expect(band.locator("[data-urgent-initiative-count]")).toContainText(
    "3 initiatives need attention",
  );
  await expect(
    page.getByRole("heading", { name: "Demo dashboard", exact: true }),
  ).toBeVisible();
  if (!BEFORE) {
    const inlineReport = page.getByRole("region", {
      name: "Visual report",
      exact: true,
    });
    await expect(
      inlineReport.getByRole("combobox", { name: "Filter by freshness" }),
    ).toHaveCount(0);
    await expect(
      inlineReport.getByRole("button", { name: "All projects" }),
    ).toHaveCount(0);
    await expect(
      inlineReport.getByRole("button", { name: "Inspect evidence" }),
    ).toHaveCount(0);
    await expect(inlineReport.locator(".report-footnote")).toHaveCount(0);
    await expect(inlineReport.locator(".report-toolbar")).toHaveCount(0);
    await expect(
      page.getByRole("link", { name: "Open document", exact: true }),
    ).toBeVisible();

    // Worst first, with the finished one folded out of the grid.
    await expect(
      page.locator('[aria-label="Open initiatives"] > li'),
    ).toHaveCount(6);
    const tiles = page.locator(
      "[data-initiative-group='attention'] [data-tile-health]",
    );
    await expect(tiles.nth(0)).toHaveAttribute("data-tile-health", "blocked");
    await expect(tiles.nth(1)).toHaveAttribute("data-tile-health", "at_risk");
    await expect(tiles.nth(2)).toHaveAttribute("data-tile-health", "stale");
    await expect(tiles.first()).toContainText("3/7");
    // A tile shows prose, never the markdown source.
    await expect(tiles.first().locator("[data-tile-excerpt]")).toHaveText(
      "A clear outcome for initiative 1.",
    );
    await expect(
      page.locator("[data-initiative-group='done'] [data-initiative-tile]"),
    ).toHaveCount(1);
    await expect(page.locator("[data-overview-detail]")).not.toHaveAttribute(
      "open",
    );
    const sections = await page
      .locator("[data-overview-section]")
      .evaluateAll((nodes) =>
        nodes.map((node) => node.dataset.overviewSection),
      );
    // Urgent first, then every initiative, then the pinned dashboard.
    expect(sections.slice(0, 3)).toEqual(["urgent", "initiatives", "reports"]);
    await expect(
      page.getByText("Archived backlog 1", { exact: true }),
    ).toHaveCount(0);
  }
  await mkdir(".screenshots/review", { recursive: true });
  await page.screenshot({
    path: `.screenshots/review/${BEFORE ? "before" : "after"}.png`,
    fullPage: true,
  });
});

test("work details stay collapsed until requested and exclude archived counts", async ({
  page,
}) => {
  test.skip(BEFORE);
  await installOverview(page);
  await page.goto(OVERVIEW);
  await expect(page.locator("[data-overview-work-total]")).not.toBeVisible();
  await page.getByText("Work detail", { exact: true }).click();
  await expect(page.locator("[data-overview-work-total]")).toContainText("7");
  await expect(page.locator("[data-overview-cell='nexus:backlog']")).toHaveText(
    "0",
  );
  await expect(page.locator("[data-overview-agents='stale']")).toContainText(
    "3",
  );
});

test("dashboard can be pinned and unpinned", async ({ page }) => {
  test.skip(BEFORE);
  const state = await installOverview(page);
  await page.goto(OVERVIEW);
  await page.getByRole("button", { name: "Pin as dashboard" }).click();
  await expect(
    page.getByRole("button", { name: "Pinned dashboard" }),
  ).toBeDisabled();
  expect(state.writes[0].document_ref).toBe("document:demo-dashboard");
  await page.getByRole("button", { name: "Use newest report" }).click();
  await expect(
    page.getByRole("button", { name: "Pin as dashboard" }),
  ).toBeEnabled();
  expect(state.writes[1].document_ref).toBeNull();
});

test("dashboard choices load only when the selector opens", async ({
  page,
}) => {
  test.skip(BEFORE);
  const state = await installOverview(page);
  await page.goto(OVERVIEW);
  await expect(page.locator("[data-overview-report]")).toBeVisible();
  expect(state.selectorReads).toBe(0);
  await page.getByRole("combobox", { name: "Report", exact: true }).focus();
  await expect(
    page
      .getByRole("combobox", { name: "Report", exact: true })
      .locator("option"),
  ).toHaveCount(2);
  expect(state.selectorReads).toBe(1);
});

test("bookmarked dashboard loads without focusing the selector", async ({
  page,
}) => {
  const state = await installOverview(page);
  await page.goto(`${OVERVIEW}?dashboard=older-dashboard`);
  await expect(
    page.getByRole("heading", { name: "Earlier dashboard", exact: true }),
  ).toBeVisible();
  const selector = page.getByRole("combobox", { name: "Report", exact: true });
  await expect(selector).toHaveValue("older-dashboard");
  await expect(selector).not.toBeFocused();
  await expect(
    page.getByRole("link", { name: "Open document", exact: true }),
  ).toHaveAttribute(
    "href",
    `${OVERVIEW.replace(/overview$/, "docs")}/older-dashboard`,
  );
  expect(state.selectorReads).toBe(1);
});

test("unknown bookmarked dashboard falls back without repeated selector reads", async ({
  page,
}) => {
  const state = await installOverview(page);
  await page.goto(`${OVERVIEW}?dashboard=missing-dashboard`);
  await expect(
    page
      .getByRole("combobox", { name: "Report", exact: true })
      .locator("option"),
  ).toHaveCount(2);
  await expect(
    page.getByRole("heading", { name: "Demo dashboard", exact: true }),
  ).toBeVisible();
  expect(state.selectorReads).toBe(1);
});

test("Overview keeps its skeleton while the snapshot loads", async ({
  page,
}) => {
  test.skip(BEFORE);
  const gate = deferred();
  await installOverview(page, { gate });
  await page.goto(OVERVIEW);
  await expect(
    page.getByRole("status", { name: "Loading overview" }),
  ).toBeVisible();
  gate.resolve();
  await expect(
    page.locator('[aria-label="Open initiatives"] > li'),
  ).toHaveCount(6);
});

test("failed snapshot reports an error", async ({ page }) => {
  test.skip(BEFORE);
  await installOverview(page, { failure: true });
  await page.goto(OVERVIEW);
  // Each section says it could not load; the band says the same thing in one
  // line rather than claiming nothing is waiting.
  await expect(
    page.getByRole("alert").filter({ hasText: "Initiatives are unavailable" }),
  ).toBeVisible();
  const band = page.locator('[data-overview-section="urgent"]');
  await expect(band).toContainText("could not be read");
});

test("compact dashboard ignores document filter state", async ({ page }) => {
  await installOverview(page);
  await page.goto(`${OVERVIEW}?reportProject=demo&reportFreshness=stale`);
  await expect(
    page.getByRole("heading", { name: "Launch readiness", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("No panels match these filters", { exact: true }),
  ).toHaveCount(0);
});
