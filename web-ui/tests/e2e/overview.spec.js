import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { deferred, installWorkspaceApi } from "../helpers/workspaceApiMock.js";

const OVERVIEW = "/o/local/w/local/overview";
const BEFORE = process.env.OVERVIEW_CAPTURE_BEFORE === "1";
const NOW = "2026-10-04T12:00:00.000Z";
const report = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Omi dashboard",
  summary:
    "Seven initiatives. Two decisions need David. Launch readiness is reported by the team.",
  generated_at: NOW,
  projects: [
    {
      id: "omi",
      title: "Omi",
      summary: "Launch preparation",
      outcome: "A useful launch with evidence",
    },
  ],
  sources: [],
  panels: [
    {
      id: "status",
      project_id: "omi",
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
  summary: `A clear outcome for initiative ${index + 1}.\n- [x] Define success\n- [x] Design\n- [x] Review\n- [ ] Implement\n- [ ] Verify\n- [ ] Pilot\n- [ ] Launch\n${index === 0 ? "Needs David: approve the pilot" : ""}`,
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
    id: "omi-dashboard",
    handle: "omi-dashboard",
    ref: "document:omi-dashboard",
    title: "Dashboard · Omi",
    state: "active",
    updated_at: NOW,
  },
  revision: { content: JSON.stringify(report), content_type: "text" },
};

async function installOverview(page, { gate = null, failure = false } = {}) {
  await installWorkspaceApi(page, {
    actors: [{ id: "david", display_name: "David", tags: ["human"] }],
    principals: [{ actor_id: "david", principal_kind: "human" }],
    boards: [
      {
        board: {
          id: "omi",
          handle: "omi",
          title: "Omi · Initiatives",
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
      items: active.map((item) => ({
        ...item,
        summary: item.summary.split("\n")[0],
        progress: { done: 3, total: 7 },
        needs:
          item.ref === active[0].ref ? ["Needs David: approve the pilot"] : [],
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
      reports: [{ ...dashboard.document, segment: "omi-dashboard", report }],
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
            },
          ],
        },
      });
    }
    if (path === "/inbox") return route.fulfill({ json: { items: asks } });
    if (path === "/docs/omi-dashboard")
      return route.fulfill({ json: dashboard });
    return route.fallback();
  });
  return state;
}

test("seeded CEO Overview screenshot and section order", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1700 });
  await installOverview(page);
  await page.goto(OVERVIEW);
  await expect(
    page.getByRole("link", { name: "Choose the launch date" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Omi dashboard", exact: true }),
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

    await expect(
      page.locator('[aria-label="Open initiatives"] > li'),
    ).toHaveCount(7);
    await expect(
      page.locator('[aria-label="Open initiatives"] > li').first(),
    ).toContainText("3/7");
    await expect(page.locator("[data-overview-detail]")).not.toHaveAttribute(
      "open",
    );
    const sections = await page
      .locator("[data-overview-section]")
      .evaluateAll((nodes) =>
        nodes.map((node) => node.dataset.overviewSection),
      );
    expect(sections.slice(0, 3)).toEqual([
      "needs-you",
      "reports",
      "initiatives",
    ]);
    await expect(
      page.getByText("Archived backlog 1", { exact: true }),
    ).toHaveCount(0);
  }
  await mkdir("../docs/review/overview", { recursive: true });
  await page.screenshot({
    path: `../docs/review/overview/${BEFORE ? "before" : "after"}.png`,
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
  expect(state.writes[0].document_ref).toBe("document:omi-dashboard");
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
  ).toHaveCount(7);
});

test("failed snapshot reports an error", async ({ page }) => {
  test.skip(BEFORE);
  await installOverview(page, { failure: true });
  await page.goto(OVERVIEW);
  await expect(
    page.getByRole("alert").filter({ hasText: "Needs you is unavailable" }),
  ).toBeVisible();
});

test("compact dashboard ignores document filter state", async ({ page }) => {
  await installOverview(page);
  await page.goto(`${OVERVIEW}?reportProject=omi&reportFreshness=stale`);
  await expect(
    page.getByRole("heading", { name: "Launch readiness", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("No panels match these filters", { exact: true }),
  ).toHaveCount(0);
});
