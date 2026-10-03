import { expect, test } from "@playwright/test";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

const OVERVIEW = "/o/local/w/local/overview";
const OBSERVED = "2026-10-04T12:00:00.000Z";

function report(title) {
  return {
    kind: "anx.visual-report",
    schema_version: 1,
    title,
    summary:
      "A seeded snapshot. It does not establish that the fleet is healthy.",
    generated_at: OBSERVED,
    projects: [
      {
        id: "studio",
        title: "Studio",
        summary: "Seeded for the overview test.",
        outcome: "Snapshot only",
      },
    ],
    sources: [],
    panels: [
      {
        id: "note",
        project_id: "studio",
        type: "explanation",
        title: "Evidence boundary",
        author: "Test",
        provenance: "reported",
        observed_at: null,
        freshness: "unavailable",
        source_ids: [],
        data: {
          text: "No live observation is attached. This does not establish health.",
        },
      },
    ],
  };
}

function document(id, title, updatedAt, body) {
  return {
    document: {
      id,
      handle: id,
      ref: `document:${id}`,
      title,
      state: "active",
      updated_at: updatedAt,
    },
    revision: {
      document_id: id,
      revision_id: `${id}-rev`,
      revision_number: 1,
      content_type: "text",
      content: body,
      created_at: updatedAt,
    },
  };
}

const fleet = document(
  "fleet-dashboard",
  "Fleet Dashboard",
  "2026-10-02T00:00:00.000Z",
  JSON.stringify(report("Fleet Dashboard")),
);
const weekly = document(
  "weekly-notes",
  "Weekly notes",
  "2026-10-04T00:00:00.000Z",
  JSON.stringify(report("Weekly notes")),
);
const plain = document(
  "plain-notes",
  "Launch notes",
  "2026-10-03T00:00:00.000Z",
  "Ordinary notes, not a report.",
);

const work = [
  {
    ref: "card:combat",
    title: "Tune combat",
    phase: "in_progress",
    source: { authority: "github" },
    updated_at: "2026-10-03T00:00:00.000Z",
    freshness: {
      status: "fresh",
      last_observed_at: OBSERVED,
      stale_after_seconds: 86_400,
    },
  },
  {
    ref: "card:trailer",
    title: "Trailer capture blocked",
    phase: "blocked",
    source: { authority: "multica" },
    next_actor: "actor-operator",
    updated_at: "2026-10-02T00:00:00.000Z",
    freshness: {
      status: "stale",
      last_observed_at: "2020-01-01T00:00:00.000Z",
      stale_after_seconds: 60,
    },
  },
  {
    ref: "card:native",
    title: "Native note",
    phase: "ready",
    source: { authority: "nexus" },
    updated_at: "2026-10-01T00:00:00.000Z",
    freshness: { status: "unknown" },
  },
];

async function installOverview(page, { failWork = false } = {}) {
  const api = await installWorkspaceApi(page, {
    actors: [
      { id: "actor-operator", display_name: "Operator", tags: ["human"] },
    ],
    principals: [{ actor_id: "actor-operator", principal_kind: "human" }],
    work,
    agents: [
      {
        id: "agent-codex",
        handle: "codex",
        display_name: "Codex",
        state: "working",
      },
      {
        id: "agent-cursor",
        handle: "cursor",
        display_name: "Cursor",
        state: "waiting_on_human",
      },
      { id: "agent-old", handle: "old", display_name: "Old", state: "stale" },
    ],
    documents: [fleet.document, weekly.document, plain.document],
  });
  if (failWork) {
    api.fail.work = { status: 503, message: "work store is down" };
  }
  const docs = {
    [fleet.document.id]: fleet,
    [weekly.document.id]: weekly,
    [plain.document.id]: plain,
  };
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = decodeURIComponent(url.pathname);
    if (request.method() === "GET" && path === "/inbox") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [
            {
              id: "ask-parry",
              status: "open",
              kind: "ask",
              title: "Approve the parry window",
              requester_label: "Gameplay",
              severity: "high",
              created_at: "2026-10-01T00:00:00.000Z",
            },
          ],
          total: 1,
        }),
      });
    }
    const docMatch = path.match(/^\/docs\/([^/]+)$/);
    if (request.method() === "GET" && docMatch && docs[docMatch[1]]) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(docs[docMatch[1]]),
      });
    }
    return route.fallback();
  });
  return api;
}

test("overview summarizes needs you, work, agents, and the preferred report", async ({
  page,
}) => {
  await installOverview(page);
  await page.goto(OVERVIEW);

  await expect(
    page.getByRole("heading", { name: "Overview", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Approve the parry window" }),
  ).toBeVisible();
  await expect(
    page.locator("[data-overview-cell='github:in_progress']"),
  ).toHaveText("1");
  await expect(
    page.locator("[data-overview-cell='multica:blocked']"),
  ).toHaveText("1");
  await expect(page.locator("[data-overview-agents='working']")).toContainText(
    "1",
  );
  await expect(page.locator("[data-overview-agents='waiting']")).toContainText(
    "1",
  );
  await expect(page.locator("[data-overview-agents='stale']")).toContainText(
    "1",
  );
  await expect(page.locator("[data-overview-freshness='stale']")).toContainText(
    "1",
  );
  await expect(
    page.locator("[data-overview-report='fleet-dashboard']"),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Fleet Dashboard", exact: true }),
  ).toBeVisible();

  await page.locator("[data-overview-cell='github:in_progress']").click();
  await expect(page).toHaveURL(/\/tasks\?source=github&phase=in_progress/);
});

test("a failed work read is unavailable rather than an empty fleet", async ({
  page,
}) => {
  await installOverview(page, { failWork: true });
  await page.goto(OVERVIEW);

  await expect(
    page.getByRole("alert").filter({ hasText: "Tasks are unavailable" }),
  ).toBeVisible();
  await expect(page.locator("[data-overview-work-total]")).toHaveCount(0);
  await expect(page.locator("[data-overview-section='work']")).toHaveAttribute(
    "data-overview-status",
    "unavailable",
  );
  await expect(
    page.getByRole("alert").filter({ hasText: "Needs you is unavailable" }),
  ).toBeVisible();
  await expect(page.locator("[data-overview-agents='working']")).toContainText(
    "1",
  );
});
