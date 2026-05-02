import { expect, test } from "@playwright/test";

/**
 * Guards card detail modal Discussion drawer + Timeline tab from hanging the UI.
 * Regression: unbounded mention/timeline refresh storms (e.g. principals fetches)
 * left the main thread wedged when switching tabs.
 */
test("card detail modal Discussion drawer and Timeline tab render without request storms", async ({
  page,
}) => {
  const pageErrors = [];
  page.on("pageerror", (err) => {
    pageErrors.push(String(err?.message ?? err));
  });
  page.on("console", (msg) => {
    if (msg.type() === "error") {
      pageErrors.push(`console.error: ${msg.text()}`);
    }
  });
  page.on("response", (response) => {
    if (response.status() >= 400) {
      pageErrors.push(`${response.status()} ${response.url()}`);
    }
  });
  const actorId = "actor-card-modal-tabs-e2e";
  const boardId = "board-modal-tabs";
  const cardThreadId = "thread-modal-card";
  const backingThreadId = "thread-modal-backing";

  let timelineRequestCount = 0;
  let principalsRequestCount = 0;

  const board = {
    id: boardId,
    title: "Modal Tabs Board",
    status: "active",
    owners: [actorId],
    thread_id: backingThreadId,
    refs: [`thread:${backingThreadId}`],
    document_refs: [],
    column_schema: [
      { key: "backlog", title: "Backlog", wip_limit: null },
      { key: "ready", title: "Ready", wip_limit: null },
      { key: "in_progress", title: "In Progress", wip_limit: null },
      { key: "blocked", title: "Blocked", wip_limit: null },
      { key: "review", title: "Review", wip_limit: null },
      { key: "done", title: "Done", wip_limit: null },
    ],
    pinned_refs: [],
    created_at: "2026-03-05T00:00:00.000Z",
    created_by: actorId,
    updated_at: "2026-03-05T00:00:00.000Z",
    updated_by: actorId,
  };

  const card = {
    id: "card-one",
    board_id: boardId,
    thread_id: cardThreadId,
    title: "Modal Tab Card",
    summary: "summary",
    related_refs: [
      "topic:topic-modal-card",
      "card:card-one",
      `thread:${cardThreadId}`,
    ],
    column_key: "ready",
    rank: "0001",
    document_ref: null,
    assignee_refs: [],
    risk: "medium",
    resolution: null,
    resolution_refs: ["artifact:artifact-modal-card"],
    due_at: null,
    definition_of_done: [],
    created_at: "2026-03-05T00:00:00.000Z",
    updated_at: "2026-03-05T00:00:00.000Z",
  };

  const threads = [
    {
      id: cardThreadId,
      type: "process",
      title: "Card Thread",
      status: "active",
      updated_at: "2026-03-05T06:00:00.000Z",
      updated_by: actorId,
      open_cards: [],
    },
    {
      id: backingThreadId,
      type: "process",
      title: "Board Backing",
      status: "active",
      updated_at: "2026-03-05T06:00:00.000Z",
      updated_by: actorId,
      open_cards: [],
    },
  ];

  const cardThread = threads.find((t) => t.id === cardThreadId);
  const backingThread = threads.find((t) => t.id === backingThreadId);
  const generatedAt = board.updated_at;
  const threadIds = new Set([backingThreadId, cardThreadId].filter(Boolean));
  const freshnessThreads = [...threadIds].sort().map((threadId) => ({
    thread_id: threadId,
    status: "current",
    generated_at: generatedAt,
    queued_at: null,
    started_at: null,
    completed_at: generatedAt,
    last_error_at: null,
    last_error: null,
    materialized: true,
    refresh_in_flight: false,
  }));

  const workspacePayload = {
    board_id: board.id,
    board,
    backing_thread: backingThread ?? null,
    cards: {
      count: 1,
      items: [
        {
          membership: card,
          backing: {
            thread_id: card.thread_id,
            thread: cardThread,
            pinned_document_ref: null,
            pinned_document: null,
          },
          derived: {
            summary: {
              open_card_count: 0,
              decision_request_count: 0,
              decision_count: 0,
              recommendation_count: 0,
              document_count: 0,
              inbox_count: 0,
              latest_activity_at: cardThread?.updated_at ?? card.updated_at,
            },
            freshness: freshnessThreads.find(
              (x) => x.thread_id === cardThreadId,
            ),
          },
        },
      ],
    },
    documents: { items: [], count: 0 },
    inbox: { items: [], count: 0, generated_at: board.updated_at },
    board_summary: {
      card_count: 1,
      cards_by_column: {
        backlog: 0,
        ready: 1,
        in_progress: 0,
        blocked: 0,
        review: 0,
        done: 0,
      },
      open_card_count: 0,
      document_count: 0,
      latest_activity_at: generatedAt,
      has_document_ref: false,
    },
    projection_freshness: {
      status: "current",
      thread_count: freshnessThreads.length,
      threads: freshnessThreads,
    },
    board_summary_freshness: {
      status: "current",
      thread_count: freshnessThreads.length,
      threads: freshnessThreads,
    },
    warnings: { items: [], count: 0 },
    section_kinds: {
      board: "canonical",
      cards: "convenience",
      documents: "derived",
      inbox: "derived",
      board_summary: "derived",
    },
    generated_at: generatedAt,
  };

  const cardTimelineEvents = [
    {
      id: "evt-card-upd",
      ts: "2026-03-03T10:00:00.000Z",
      type: "card_updated",
      actor_id: actorId,
      thread_id: cardThreadId,
      refs: [`thread:${cardThreadId}`, `board:${boardId}`, "card:card-one"],
      summary: "Card updated: Modal Tab Card",
      payload: {
        card_id: "card-one",
        board_id: boardId,
        changed_fields: ["title"],
        previous_title: "Old",
        title: "Modal Tab Card",
      },
      provenance: { sources: ["event:ui"] },
    },
  ];

  const threadTimelineEvents = [
    {
      id: "evt-modal-1",
      ts: "2026-03-03T10:00:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: cardThreadId,
      refs: [`thread:${cardThreadId}`],
      summary: "Message: hello from modal card thread",
      payload: { text: "hello from modal card thread" },
      provenance: { sources: ["event:ui"] },
    },
  ];

  const cardTimelineBody = JSON.stringify({
    events: cardTimelineEvents,
    artifacts: {},
    documents: {},
    document_revisions: {},
    cards: [],
    threads: [],
  });

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.context().addCookies([
    {
      name: "anx_ui_session_local",
      value: "test-refresh-token",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);

  await page.route("**/auth/session", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        authenticated: true,
        agent: {
          agent_id: "agent-modal-tabs",
          actor_id: actorId,
          username: "modal-tabs",
        },
      }),
    });
  });

  await page.route("**/auth/dev/identities", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ personas: [] }),
    });
  });

  await page.route(/\/auth\/principals(?:\?.*)?$/, async (route) => {
    principalsRequestCount += 1;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ principals: [], next_cursor: "" }),
    });
  });

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Card Modal Tabs Tester" }],
      }),
    });
  });

  await page.route(
    (url) =>
      url.pathname.includes(`${boardId}/workspace`) &&
      url.pathname.includes("/boards/"),
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(workspacePayload),
      });
    },
  );

  await page.route(
    (url) =>
      url.pathname.includes(`/cards/card-one/timeline`) ||
      url.pathname.endsWith(`/cards/card-one/timeline`),
    async (route) => {
      timelineRequestCount += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: cardTimelineBody,
      });
    },
  );

  await page.route(
    (url) =>
      url.pathname.includes(`/threads/${cardThreadId}/timeline`) ||
      url.pathname.endsWith(`/threads/${cardThreadId}/timeline`),
    async (route) => {
      timelineRequestCount += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ events: threadTimelineEvents }),
      });
    },
  );

  await page.route(
    (url) =>
      url.pathname.includes(`/cards/card-one/revisions`) ||
      url.pathname.endsWith(`/cards/card-one/revisions`),
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          card_id: "card-one",
          revisions: [
            {
              revision_id: "rev-2",
              revision_number: 2,
              title: "Modal Tab Card",
              summary: "summary v2",
              artifact_ref: "artifact:artifact-modal-card",
              created_at: "2026-03-06T00:00:00.000Z",
              created_by: actorId,
              definition_of_done: [],
            },
            {
              revision_id: "rev-1",
              revision_number: 1,
              title: "Older",
              summary: "summary v1",
              artifact_ref: "artifact:artifact-modal-card",
              created_at: "2026-03-05T00:00:00.000Z",
              created_by: actorId,
              definition_of_done: [],
            },
          ],
        }),
      });
    },
  );

  await page.goto(`/o/local/w/local/boards/${boardId}`);
  await page.waitForLoadState("networkidle");

  await expect(
    page.getByRole("heading", { name: "Modal Tabs Board" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Manage Modal Tab Card" }).click();

  const dialogCount = await page
    .locator('[role="dialog"][aria-label="Card details"]')
    .count();
  expect(dialogCount, "expected single card modal in DOM").toBe(1);

  let dialog = page.getByRole("dialog", { name: "Card details" });
  await expect(dialog).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => document.body.style.position))
    .toBe("fixed");
  await expect(
    dialog.locator(".cdm-panel"),
    "modal panel should establish an internal scrollport",
  ).toHaveCSS("overflow", "hidden");
  await expect(dialog.locator(".cdm-scroll")).toHaveCSS("overflow-y", "auto");

  await expect(page).toHaveURL(
    new RegExp(`[?&]card=${encodeURIComponent("card-one")}`),
  );
  await page.reload();
  await page.waitForLoadState("networkidle");
  dialog = page.getByRole("dialog", { name: "Card details" });
  await expect(dialog).toBeVisible({
    timeout: 15_000,
  });

  await expect(
    dialog.getByRole("link", { name: /topic-modal-card/ }).first(),
  ).toHaveAttribute("href", "/o/local/w/local/topics/topic-modal-card");
  await expect(
    dialog.getByRole("link", { name: /card-one/ }).first(),
  ).toHaveAttribute("href", `/o/local/w/local/boards/${boardId}?card=card-one`);

  const tabCount = await dialog
    .locator('[aria-label="Card sections"] [role="tab"]')
    .count();
  expect(tabCount, "expected 4 section tabs in modal").toBe(4);

  await expect(dialog.getByTestId("cdm-tab-resolution")).toHaveText(
    /resolution/i,
  );

  await dialog.getByTestId("cdm-tab-resolution").click();
  await expect(page).toHaveURL(/tab=resolution/);
  await expect(dialog.getByTestId("cdm-section-tab-val")).toHaveText(
    "resolution",
    { timeout: 15_000 },
  );
  await expect(
    dialog
      .locator('[data-cdm-panel="resolution"]')
      .getByText("Definition of done", { exact: false }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("link", { name: /artifact-modal-card/ }).first(),
  ).toHaveAttribute("href", "/o/local/w/local/artifacts/artifact-modal-card");

  await dialog.getByRole("tab", { name: /^Overview\b/i }).click();
  await expect(dialog.getByTestId("cdm-section-tab-val")).toHaveText(
    "overview",
    { timeout: 5_000 },
  );

  const principalCountBefore = principalsRequestCount;
  const timelineCountBefore = timelineRequestCount;

  await dialog.getByRole("button", { name: /^Discussion\b/i }).click();

  await expect(dialog.getByTestId("cdm-section-tab-val")).toHaveText(
    "overview",
    {
      timeout: 5_000,
    },
  );

  await expect(dialog.getByRole("textbox", { name: "Message" })).toBeVisible({
    timeout: 15_000,
  });

  await expect
    .poll(() => timelineRequestCount, { timeout: 10_000 })
    .toBeGreaterThan(0);
  expect(pageErrors, `page errors: ${pageErrors.join("\n")}`).toEqual([]);
  await expect(
    dialog.getByText("hello from modal card thread", { exact: false }),
  ).toBeVisible();

  await dialog.getByTestId("cdm-tab-timeline").click();
  await expect(page).toHaveURL(/tab=timeline/);
  await expect(dialog.getByTestId("cdm-section-tab-val")).toHaveText(
    "timeline",
    { timeout: 15_000 },
  );

  await expect(dialog.getByText(/Card updated/i).first()).toBeVisible();
  await expect(dialog.getByText(/Title:/i).first()).toBeVisible();

  await dialog.getByRole("tab", { name: /^Revisions\b/i }).click();
  await expect(page).toHaveURL(/tab=revisions/);
  await expect(dialog.getByTestId("cdm-section-tab-val")).toHaveText(
    "revisions",
    { timeout: 15_000 },
  );
  await expect(
    dialog.getByText("View:", { exact: true }).first(),
  ).toBeVisible();

  const principalsDelta = principalsRequestCount - principalCountBefore;
  const timelineDelta = timelineRequestCount - timelineCountBefore;

  expect(
    timelineDelta,
    `expected bounded thread timeline fetches after tab switches, got ${timelineDelta}`,
  ).toBeLessThanOrEqual(20);

  expect(
    principalsDelta,
    `expected bounded /auth/principals fetches (mention + layout), got ${principalsDelta}`,
  ).toBeLessThanOrEqual(24);
});
