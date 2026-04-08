import { expect, test } from "@playwright/test";

import { mockTopicRefFromThreadId } from "../../src/lib/devSeedData.js";

const actorId = "actor-board-e2e";
const columns = [
  { key: "backlog", title: "Backlog", wip_limit: null },
  { key: "ready", title: "Ready", wip_limit: null },
  { key: "in_progress", title: "In Progress", wip_limit: null },
  { key: "blocked", title: "Blocked", wip_limit: null },
  { key: "review", title: "Review", wip_limit: null },
  { key: "done", title: "Done", wip_limit: null },
];

function backingThreadId(board) {
  return String(board?.thread_id ?? "").trim();
}

function firstDocumentIdFromBoard(board) {
  for (const ref of board?.document_refs ?? []) {
    const s = String(ref ?? "").trim();
    if (s.startsWith("document:")) {
      return s.slice("document:".length).trim();
    }
    if (s && !s.includes(":")) {
      return s;
    }
  }
  for (const ref of board?.refs ?? []) {
    const s = String(ref ?? "").trim();
    if (s.startsWith("document:")) {
      return s.slice("document:".length).trim();
    }
  }
  return "";
}

function buildSummary(board, cards, threads, documents) {
  const cardsByColumn = Object.fromEntries(
    columns.map((column) => [column.key, 0]),
  );
  const threadIds = new Set([backingThreadId(board)].filter(Boolean));
  let latestActivityAt = board.updated_at;
  let openCardCount = 0;
  let documentCount = 0;

  cards.forEach((card) => {
    cardsByColumn[card.column_key] = (cardsByColumn[card.column_key] ?? 0) + 1;
    const tid = String(card.thread_id ?? "").trim();
    if (tid) threadIds.add(tid);
  });

  for (const threadId of threadIds) {
    const thread = threads.find((item) => item.id === threadId);
    if (
      thread?.updated_at &&
      Date.parse(thread.updated_at) > Date.parse(latestActivityAt)
    ) {
      latestActivityAt = thread.updated_at;
    }
    openCardCount += Array.isArray(thread?.open_cards)
      ? thread.open_cards.length
      : 0;
    documentCount += documents.filter(
      (document) => document.thread_id === threadId,
    ).length;
  }

  return {
    card_count: cards.length,
    cards_by_column: cardsByColumn,
    open_card_count: openCardCount,
    document_count: documentCount,
    latest_activity_at: latestActivityAt,
    has_document_ref: Boolean(firstDocumentIdFromBoard(board)),
  };
}

function sortCards(cards) {
  const order = new Map(columns.map((column, index) => [column.key, index]));
  return [...cards].sort((left, right) => {
    const columnDelta =
      (order.get(left.column_key) ?? columns.length) -
      (order.get(right.column_key) ?? columns.length);
    if (columnDelta !== 0) return columnDelta;

    const rankDelta =
      Number.parseInt(left.rank ?? "0", 10) -
      Number.parseInt(right.rank ?? "0", 10);
    if (rankDelta !== 0) return rankDelta;

    return String(left.thread_id || left.id).localeCompare(
      String(right.thread_id || right.id),
    );
  });
}

function buildWorkspace(
  board,
  cards,
  threads,
  documents,
  inboxItems,
  options = {},
) {
  const sortedCards = sortCards(cards);
  const threadIds = new Set([backingThreadId(board)].filter(Boolean));
  for (const card of cards) {
    const tid = String(card.thread_id ?? "").trim();
    if (tid) threadIds.add(tid);
  }
  const workspaceDocuments = documents.filter((document) =>
    threadIds.has(document.thread_id),
  );
  const workspaceInbox = inboxItems.filter((item) =>
    threadIds.has(item.thread_id),
  );
  const projectionStatus = options.projectionStatus ?? "current";
  const generatedAt = options.generatedAt ?? board.updated_at;
  const freshnessThreads = [...threadIds].sort().map((threadId) => ({
    thread_id: threadId,
    status: projectionStatus,
    generated_at: generatedAt,
    queued_at: projectionStatus === "pending" ? generatedAt : null,
    started_at: null,
    completed_at: projectionStatus === "current" ? generatedAt : null,
    last_error_at: projectionStatus === "error" ? generatedAt : null,
    last_error:
      projectionStatus === "error" ? "projection refresh failed" : null,
    materialized: projectionStatus !== "missing",
    refresh_in_flight: projectionStatus === "pending",
  }));

  return {
    board_id: board.id,
    board,
    backing_thread:
      threads.find((thread) => thread.id === backingThreadId(board)) ?? null,
    cards: {
      items: sortedCards.map((card) => ({
        membership: card,
        backing: {
          thread_id: card.thread_id,
          thread: threads.find((thread) => thread.id === card.thread_id),
          pinned_document_ref: (() => {
            const docId = String(card.document_ref ?? "")
              .replace(/^document:/, "")
              .trim();
            return docId ? `document:${docId}` : null;
          })(),
          pinned_document:
            documents.find((document) => {
              const docId = String(card.document_ref ?? "")
                .replace(/^document:/, "")
                .trim();
              return document.id === docId;
            }) ?? null,
        },
        derived: {
          summary: {
            open_card_count:
              threads.find((thread) => thread.id === card.thread_id)?.open_cards
                ?.length ?? 0,
            decision_request_count: workspaceInbox.filter(
              (item) =>
                item.thread_id === card.thread_id &&
                item.category === "decision_needed",
            ).length,
            decision_count: 0,
            recommendation_count: 0,
            document_count: workspaceDocuments.filter(
              (document) => document.thread_id === card.thread_id,
            ).length,
            inbox_count: workspaceInbox.filter(
              (item) => item.thread_id === card.thread_id,
            ).length,
            latest_activity_at:
              threads.find((thread) => thread.id === card.thread_id)
                ?.updated_at ?? card.updated_at,
            stale:
              threads.find((thread) => thread.id === card.thread_id)
                ?.staleness === "stale" ||
              threads.find((thread) => thread.id === card.thread_id)
                ?.staleness === "very-stale",
          },
          freshness: freshnessThreads.find(
            (item) => item.thread_id === card.thread_id,
          ),
        },
      })),
      count: cards.length,
    },
    documents: {
      items: workspaceDocuments,
      count: workspaceDocuments.length,
    },
    inbox: {
      items: workspaceInbox,
      count: workspaceInbox.length,
      generated_at: board.updated_at,
    },
    board_summary: buildSummary(board, cards, threads, documents),
    projection_freshness: {
      status: projectionStatus,
      thread_count: freshnessThreads.length,
      threads: freshnessThreads,
    },
    board_summary_freshness: {
      status: projectionStatus,
      thread_count: freshnessThreads.length,
      threads: freshnessThreads,
    },
    warnings: {
      items: [
        {
          thread_id: "thread-review",
          message: "card pinned document is no longer available",
        },
      ],
      count: 1,
    },
    section_kinds: {
      board: "canonical",
      cards: "convenience",
      documents: "derived",
      inbox: "derived",
      board_summary: "derived",
    },
    generated_at: generatedAt,
  };
}

function renormalize(cards, columnKey) {
  cards
    .filter((card) => card.column_key === columnKey)
    .forEach((card, index) => {
      card.rank = String(index + 1).padStart(4, "0");
    });
}

test("board UI supports create/edit and card mutation flows", async ({
  page,
}) => {
  const threads = [
    {
      id: "thread-primary",
      type: "process",
      title: "Primary Coordination Thread",
      status: "active",
      priority: "p1",
      updated_at: "2026-03-06T08:00:00.000Z",
      updated_by: actorId,
      open_cards: ["card-primary"],
    },
    {
      id: "thread-execution",
      type: "process",
      title: "Execution Track",
      status: "active",
      priority: "p2",
      updated_at: "2026-03-05T06:00:00.000Z",
      updated_by: actorId,
      open_cards: ["card-execution"],
    },
    {
      id: "thread-review",
      type: "process",
      title: "Review Prep",
      status: "active",
      priority: "p2",
      updated_at: "2026-03-05T07:00:00.000Z",
      updated_by: actorId,
      open_cards: [],
      staleness: "stale",
    },
  ];
  const topicSearchRecords = threads.map((th) => ({
    id: th.id,
    thread_id: th.id,
    title: th.title,
    type: th.type,
    status: th.status,
    summary: "",
    owner_refs: [],
    document_refs: [],
    board_refs: [],
    related_refs: [],
    provenance: { sources: ["inferred"] },
  }));
  const documents = [
    {
      id: "doc-runbook",
      title: "Launch Runbook",
      thread_id: "thread-primary",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      status: "active",
      labels: [],
      head_revision_id: "rev-doc-runbook-1",
      head_revision_number: 1,
    },
    {
      id: "doc-playbook",
      title: "Incident Playbook",
      thread_id: "thread-execution",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      status: "active",
      labels: [],
      head_revision_id: "rev-doc-playbook-1",
      head_revision_number: 1,
    },
  ];
  const inboxItems = [
    {
      id: "inbox-review-1",
      thread_id: "thread-review",
      category: "decision_needed",
      title: "Need sign-off on review prep",
      refs: ["thread:thread-review"],
      source_event_time: "2026-03-05T05:30:00.000Z",
    },
  ];

  let board = null;
  let cards = [];
  let mutationCounter = 0;
  const boardCreatePayloads = [];
  const boardPatchPayloads = [];
  const addCardPayloads = [];
  const movePayloads = [];
  const updateCardPayloads = [];
  const removePayloads = [];

  function nextTimestamp() {
    mutationCounter += 1;
    return `2026-03-05T0${mutationCounter}:00:00.000Z`;
  }

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Board Tester" }],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ threads }),
    });
  });

  await page.route(
    (url) => url.pathname.endsWith("/topics"),
    async (route) => {
      const request = route.request();
      if (request.method() !== "GET") {
        await route.continue();
        return;
      }
      const url = new URL(request.url());
      const q = (url.searchParams.get("q") || "").trim().toLowerCase();
      const filtered = topicSearchRecords.filter((topic) => {
        if (!q) return true;
        const hay =
          `${topic.id} ${topic.title || ""} ${topic.summary || ""}`.toLowerCase();
        return hay.includes(q);
      });
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ topics: filtered }),
      });
    },
  );

  await page.route(/\/docs(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ documents }),
    });
  });

  await page.route(/\/boards$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          boards: board
            ? [
                {
                  board,
                  summary: buildSummary(board, cards, threads, documents),
                },
              ]
            : [],
        }),
      });
      return;
    }

    const payload = JSON.parse(request.postData() ?? "{}");
    boardCreatePayloads.push(payload);
    const threadId = String(payload.board.thread_id ?? "").trim();
    const docRefs = Array.isArray(payload.board.document_refs)
      ? payload.board.document_refs.map((r) => String(r ?? "").trim())
      : [];
    board = {
      id: "board-created",
      title: payload.board.title,
      status: payload.board.status,
      labels: payload.board.labels ?? [],
      owners: payload.board.owners ?? [actorId],
      thread_id: threadId,
      refs: [
        ...new Set([
          ...(threadId
            ? [`thread:${threadId}`, mockTopicRefFromThreadId(threadId)]
            : []),
          ...docRefs,
          ...(payload.board.pinned_refs ?? []),
        ]),
      ].sort(),
      document_refs: docRefs,
      column_schema: columns,
      pinned_refs: payload.board.pinned_refs ?? [],
      created_at: "2026-03-05T00:00:00.000Z",
      created_by: actorId,
      updated_at: "2026-03-05T00:00:00.000Z",
      updated_by: actorId,
    };

    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({ board }),
    });
  });

  await page.route(/\/boards\/board-created\/workspace$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        buildWorkspace(board, cards, threads, documents, inboxItems),
      ),
    });
  });

  await page.route(/\/boards\/board-created$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    const payload = JSON.parse(request.postData() ?? "{}");
    boardPatchPayloads.push(payload);
    board = {
      ...board,
      ...payload.patch,
      updated_at: nextTimestamp(),
      updated_by: actorId,
    };

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ board }),
    });
  });

  await page.route(/\/boards\/board-created\/cards$/, async (route) => {
    const payload = JSON.parse(route.request().postData() ?? "{}");
    addCardPayloads.push(payload);
    const now = nextTimestamp();
    const threadToken = (payload.related_refs ?? []).find((ref) =>
      String(ref ?? "").startsWith("thread:"),
    );
    const threadId = String(threadToken ?? "")
      .replace(/^thread:/, "")
      .trim();
    const title = String(payload.title ?? "").trim();
    const targetColumnCards = cards.filter(
      (card) => card.column_key === payload.column_key,
    );
    const docFromPayload = String(payload.document_ref ?? "")
      .replace(/^document:/, "")
      .trim();
    const newCard = {
      id: threadId,
      board_id: "board-created",
      thread_id: threadId,
      title,
      summary: String(payload.summary ?? "").trim() || title,
      related_refs: Array.isArray(payload.related_refs)
        ? payload.related_refs
        : [],
      column_key: payload.column_key,
      rank: String(targetColumnCards.length + 1).padStart(4, "0"),
      document_ref: docFromPayload ? `document:${docFromPayload}` : null,
      created_at: now,
      created_by: actorId,
      updated_at: now,
      updated_by: actorId,
    };
    cards.push(newCard);
    board = {
      ...board,
      updated_at: now,
      updated_by: actorId,
    };

    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({ board, card: newCard }),
    });
  });

  await page.route(
    /\/boards\/board-created\/cards\/(thread-execution|thread-review)\/move$/,
    async (route) => {
      const cardId = route
        .request()
        .url()
        .match(/cards\/([^/]+)\/move$/)?.[1];
      const payload = JSON.parse(route.request().postData() ?? "{}");
      movePayloads.push({ cardId, payload });

      const movingCard = cards.find(
        (card) => card.thread_id === cardId || card.id === cardId,
      );
      const groupedCards = Object.fromEntries(
        columns.map((column) => [column.key, []]),
      );

      cards
        .filter((card) => card.thread_id !== cardId && card.id !== cardId)
        .forEach((card) => {
          groupedCards[card.column_key].push(card);
        });

      for (const columnCards of Object.values(groupedCards)) {
        columnCards.sort(
          (left, right) =>
            Number.parseInt(left.rank ?? "0", 10) -
            Number.parseInt(right.rank ?? "0", 10),
        );
      }

      movingCard.column_key = payload.column_key;
      if (payload.column_key === "done") {
        movingCard.resolution =
          payload.resolution === "canceled" ? "canceled" : "done";
        movingCard.resolution_refs = Array.isArray(payload.resolution_refs)
          ? payload.resolution_refs
          : [];
      }
      const targetCards = groupedCards[payload.column_key];
      let insertIndex = targetCards.length;
      if (payload.before_card_id) {
        insertIndex = targetCards.findIndex(
          (card) =>
            card.id === payload.before_card_id ||
            card.thread_id === payload.before_card_id,
        );
      } else if (payload.after_card_id) {
        const afterIndex = targetCards.findIndex(
          (card) =>
            card.id === payload.after_card_id ||
            card.thread_id === payload.after_card_id,
        );
        insertIndex = afterIndex >= 0 ? afterIndex + 1 : targetCards.length;
      }

      if (insertIndex < 0) insertIndex = targetCards.length;

      targetCards.splice(insertIndex, 0, movingCard);
      cards = columns.flatMap((column) => groupedCards[column.key]);
      for (const column of columns) {
        renormalize(cards, column.key);
      }
      movingCard.updated_at = nextTimestamp();
      movingCard.updated_by = actorId;
      board = {
        ...board,
        updated_at: movingCard.updated_at,
        updated_by: actorId,
      };

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ board, card: movingCard }),
      });
    },
  );

  await page.route(/\/cards\/thread-execution$/, async (route) => {
    if (route.request().method() !== "PATCH") {
      await route.continue();
      return;
    }
    const payload = JSON.parse(route.request().postData() ?? "{}");
    updateCardPayloads.push(payload);
    const card = cards.find((item) => item.thread_id === "thread-execution");
    const docId = String(payload.patch?.document_ref ?? "")
      .replace(/^document:/, "")
      .trim();
    card.document_ref = docId ? `document:${docId}` : null;
    if (typeof payload.patch?.title === "string") {
      card.title = String(payload.patch.title).trim();
    }
    if (typeof payload.patch?.summary === "string") {
      card.summary = String(payload.patch.summary).trim();
    }
    if (Array.isArray(payload.patch?.related_refs)) {
      card.related_refs = payload.patch.related_refs;
    }
    card.updated_at = nextTimestamp();
    card.updated_by = actorId;
    board = {
      ...board,
      updated_at: card.updated_at,
      updated_by: actorId,
    };

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ board, card }),
    });
  });

  await page.route(/\/cards\/thread-execution\/archive$/, async (route) => {
    const payload = JSON.parse(route.request().postData() ?? "{}");
    removePayloads.push(payload);
    const removed = cards.find((card) => card.thread_id === "thread-execution");
    cards = cards.filter((card) => card.thread_id !== "thread-execution");
    renormalize(cards, "ready");
    board = {
      ...board,
      updated_at: nextTimestamp(),
      updated_by: actorId,
    };

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ board, card: removed }),
    });
  });

  await page.goto("/local/boards");
  await page.waitForLoadState("networkidle");

  await expect(page.getByRole("heading", { name: "Boards" })).toBeVisible();
  await page.getByRole("button", { name: "Create board", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Hide create form" }),
  ).toBeVisible();
  await page.getByLabel("Board title").fill("Launch Control");
  await page.getByLabel("Status").selectOption("paused");
  await page.getByLabel("Board timeline search").fill("Primary Coordination");
  await page
    .getByRole("button", { name: /Primary Coordination Thread/ })
    .click();
  await page.getByLabel("Board document search").fill("Launch Runbook");
  await page.getByRole("button", { name: /Launch Runbook/ }).click();
  await page.getByRole("button", { name: "Create board", exact: true }).click();

  await expect(page).toHaveURL(/\/local\/boards\/board-created$/);
  await expect(
    page.getByRole("heading", { name: "Launch Control" }),
  ).toBeVisible();
  await expect(page.getByText("Paused", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Workspace docs" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Resolved cards" }),
  ).toBeVisible();
  await expect(page.getByText("Review inbox", { exact: true })).toBeVisible();
  await expect(page.getByText("Warnings", { exact: true })).toBeVisible();
  expect(boardCreatePayloads).toEqual([
    {
      actor_id: actorId,
      board: {
        title: "Launch Control",
        status: "paused",
        thread_id: "thread-primary",
        document_refs: ["document:doc-runbook"],
      },
    },
  ]);

  await page.getByRole("button", { name: "Edit" }).click();
  await page.getByLabel("Board title").fill("Launch Control v2");
  await page.getByLabel("Status").selectOption("closed");
  await page.getByLabel("Board document search").fill("Incident Playbook");
  await page.getByRole("button", { name: /Incident Playbook/ }).click();
  await page.getByRole("button", { name: "Save board" }).click();

  await expect(
    page.getByRole("heading", { name: "Launch Control v2" }),
  ).toBeVisible();
  await expect(page.getByText("Closed", { exact: true })).toBeVisible();
  expect(boardPatchPayloads).toEqual([
    {
      actor_id: actorId,
      if_updated_at: "2026-03-05T00:00:00.000Z",
      patch: {
        title: "Launch Control v2",
        status: "closed",
        document_refs: ["document:doc-playbook"],
        labels: [],
        owners: [actorId],
        pinned_refs: [],
      },
    },
  ]);

  await page.getByRole("button", { name: "Add card", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Board timeline search" })
    .fill("Execution Track");
  await page.getByRole("button", { name: /Execution Track/ }).click();
  await page.getByLabel("Target column").selectOption("ready");
  await page
    .getByRole("textbox", { name: "Document search" })
    .fill("Incident Playbook");
  await page.getByRole("button", { name: /Incident Playbook/ }).click();
  await page.getByRole("button", { name: "Add card", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Manage Execution Track" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Add card", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Board timeline search" })
    .fill("Review Prep");
  await page.getByRole("button", { name: /Review Prep/ }).click();
  await page.getByLabel("Target column").selectOption("ready");
  await page.getByRole("button", { name: "Add card", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Manage Review Prep" }),
  ).toBeVisible();
  await expect(
    page.locator("#card-thread-review").locator(".text-orange-400"),
  ).toBeVisible();
  await expect(page.getByText("Need sign-off on review prep")).toBeVisible();

  await page.getByRole("button", { name: "Manage Review Prep" }).click();
  await page.getByRole("button", { name: "Move up" }).click();

  const readySection = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "Ready" }) });
  await expect(readySection.locator("#card-thread-execution")).toContainText(
    "Execution Track",
  );
  await expect(readySection.locator("#card-thread-review")).toContainText(
    "Review Prep",
  );

  await page.getByLabel("Column").selectOption("done");
  await page.getByRole("button", { name: "Move", exact: true }).click();

  await page
    .getByRole("dialog", { name: "Card details" })
    .getByRole("button", { name: "Close" })
    .click();

  await page.getByRole("button", { name: "Done 1" }).click();
  await expect(
    page.getByRole("button", { name: "Manage Review Prep" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Manage Execution Track" }).click();
  const executionDialog = page.getByRole("dialog", { name: "Card details" });
  await executionDialog.getByRole("button", { name: "Edit card" }).click();
  await executionDialog.getByLabel("Document ID").fill("doc-playbook");
  await executionDialog
    .getByRole("button", { name: "Save card details" })
    .click();

  await executionDialog.getByRole("button", { name: "Remove card" }).click();
  await expect(
    readySection.getByRole("button", { name: "Manage Execution Track" }),
  ).toHaveCount(0);

  expect(addCardPayloads).toEqual([
    {
      actor_id: actorId,
      if_board_updated_at: "2026-03-05T01:00:00.000Z",
      title: "Execution Track",
      summary: "Execution Track",
      column_key: "ready",
      document_ref: "document:doc-playbook",
      assignee_refs: [],
      risk: "medium",
      resolution: null,
      resolution_refs: [],
      related_refs: ["thread:thread-execution"],
      due_at: null,
      definition_of_done: [],
    },
    {
      actor_id: actorId,
      if_board_updated_at: "2026-03-05T02:00:00.000Z",
      title: "Review Prep",
      summary: "Review Prep",
      column_key: "ready",
      document_ref: null,
      assignee_refs: [],
      risk: "medium",
      resolution: null,
      resolution_refs: [],
      related_refs: ["thread:thread-review"],
      due_at: null,
      definition_of_done: [],
    },
  ]);
  expect(movePayloads).toEqual([
    {
      cardId: "thread-review",
      payload: {
        actor_id: actorId,
        if_board_updated_at: "2026-03-05T03:00:00.000Z",
        column_key: "ready",
        before_card_id: "thread-execution",
      },
    },
    {
      cardId: "thread-review",
      payload: {
        actor_id: actorId,
        if_board_updated_at: "2026-03-05T04:00:00.000Z",
        column_key: "done",
        resolution: "done",
      },
    },
  ]);
  expect(updateCardPayloads).toEqual([
    {
      actor_id: actorId,
      if_board_updated_at: "2026-03-05T05:00:00.000Z",
      patch: {
        title: "Execution Track",
        summary: "Execution Track",
        document_ref: "document:doc-playbook",
        assignee_refs: [],
        risk: "medium",
        resolution: null,
        resolution_refs: [],
        related_refs: ["thread:thread-execution"],
        due_at: null,
        definition_of_done: [],
      },
    },
  ]);
  expect(removePayloads).toEqual([
    {
      actor_id: actorId,
      if_board_updated_at: "2026-03-05T06:00:00.000Z",
    },
  ]);
});

test("board detail shows pending freshness and hides derived card counts until refreshed", async ({
  page,
}) => {
  const threads = [
    {
      id: "thread-primary",
      type: "process",
      title: "Primary Coordination Thread",
      status: "active",
      priority: "p1",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      open_cards: ["card-primary"],
    },
    {
      id: "thread-execution",
      type: "process",
      title: "Execution Track",
      status: "active",
      priority: "p2",
      updated_at: "2026-03-04T01:00:00.000Z",
      updated_by: actorId,
      open_cards: ["card-execution"],
    },
  ];
  const documents = [
    {
      id: "doc-runbook",
      title: "Launch Runbook",
      thread_id: "thread-primary",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      status: "active",
      labels: [],
      head_revision_id: "rev-doc-runbook-1",
      head_revision_number: 1,
    },
  ];
  const board = {
    id: "board-pending",
    title: "Pending Board",
    status: "active",
    labels: [],
    owners: [actorId],
    thread_id: "thread-primary",
    refs: [
      "document:doc-runbook",
      "thread:thread-primary",
      mockTopicRefFromThreadId("thread-primary"),
    ],
    document_refs: ["document:doc-runbook"],
    column_schema: columns,
    pinned_refs: [],
    created_at: "2026-03-04T00:00:00.000Z",
    created_by: actorId,
    updated_at: "2026-03-04T00:00:00.000Z",
    updated_by: actorId,
  };
  const cards = [
    {
      id: "thread-execution",
      board_id: "board-pending",
      thread_id: "thread-execution",
      column_key: "ready",
      rank: "0001",
      document_ref: null,
      created_at: "2026-03-04T00:00:00.000Z",
      created_by: actorId,
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Board Tester" }],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ threads }),
    });
  });

  await page.route(/\/docs(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ documents }),
    });
  });

  await page.route(/\/boards\/board-pending\/workspace$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        buildWorkspace(board, cards, threads, documents, [], {
          projectionStatus: "pending",
        }),
      ),
    });
  });

  await page.goto("/local/boards/board-pending");

  await expect(
    page.getByRole("heading", { name: "Pending Board" }),
  ).toBeVisible();
  await expect(page.getByText("Pending refresh", { exact: true })).toHaveCount(
    2,
  );
  await expect(
    page.locator('[title*="Derived summaries are being refreshed"]'),
  ).toBeVisible();
  await expect(page.getByText("1 inbox", { exact: true })).toHaveCount(0);
});

test("board edit conflict reloads latest state and allows retry", async ({
  page,
}) => {
  const threads = [
    {
      id: "thread-primary",
      type: "process",
      title: "Primary Coordination Thread",
      status: "active",
      priority: "p1",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      open_cards: [],
    },
  ];
  const documents = [
    {
      id: "doc-runbook",
      title: "Launch Runbook",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      status: "active",
      labels: [],
      head_revision_id: "rev-doc-runbook-1",
      head_revision_number: 1,
    },
  ];

  let board = {
    id: "board-conflict",
    title: "Conflict Board",
    status: "active",
    labels: [],
    owners: [actorId],
    thread_id: "thread-primary",
    refs: [
      "document:doc-runbook",
      "thread:thread-primary",
      mockTopicRefFromThreadId("thread-primary"),
    ],
    document_refs: ["document:doc-runbook"],
    column_schema: columns,
    pinned_refs: [],
    created_at: "2026-03-04T00:00:00.000Z",
    created_by: actorId,
    updated_at: "2026-03-04T00:00:00.000Z",
    updated_by: actorId,
  };
  const patchPayloads = [];
  let patchAttempt = 0;

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Board Tester" }],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ threads }),
    });
  });

  await page.route(/\/docs(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ documents }),
    });
  });

  await page.route(/\/boards\/board-conflict\/workspace$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(buildWorkspace(board, [], threads, documents, [])),
    });
  });

  await page.route(/\/boards\/board-conflict$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    const payload = JSON.parse(request.postData() ?? "{}");
    patchPayloads.push(payload);
    patchAttempt += 1;

    if (patchAttempt === 1) {
      board = {
        ...board,
        title: "Server board title",
        updated_at: "2026-03-04T02:00:00.000Z",
      };
      await route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          error: "Board has been updated by another actor.",
          current: board,
        }),
      });
      return;
    }

    board = {
      ...board,
      ...payload.patch,
      updated_at: "2026-03-04T03:00:00.000Z",
      updated_by: actorId,
    };
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ board }),
    });
  });

  await page.goto("/local/boards/board-conflict");

  await expect(
    page.getByRole("heading", { name: "Conflict Board" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Edit" }).click();
  await page.getByLabel("Board title").fill("Conflict Board Edited");
  await page.getByRole("button", { name: "Save board" }).click();

  await expect(
    page.getByText("Board was updated elsewhere.", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Server board title" }),
  ).toBeVisible();

  await page.getByLabel("Board title").fill("Recovered Board Title");
  await page.getByRole("button", { name: "Save board" }).click();

  await expect(page.getByText("Board updated.", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Recovered Board Title" }),
  ).toBeVisible();

  expect(patchPayloads).toEqual([
    {
      actor_id: actorId,
      if_updated_at: "2026-03-04T00:00:00.000Z",
      patch: {
        title: "Conflict Board Edited",
        status: "active",
        document_refs: ["document:doc-runbook"],
        labels: [],
        owners: [actorId],
        pinned_refs: [],
      },
    },
    {
      actor_id: actorId,
      if_updated_at: "2026-03-04T02:00:00.000Z",
      patch: {
        title: "Recovered Board Title",
        status: "active",
        document_refs: ["document:doc-runbook"],
        labels: [],
        owners: [actorId],
        pinned_refs: [],
      },
    },
  ]);
});
