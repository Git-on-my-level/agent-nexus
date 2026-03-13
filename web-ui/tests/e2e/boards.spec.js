import { expect, test } from "@playwright/test";

const actorId = "actor-board-e2e";
const columns = [
  { key: "backlog", title: "Backlog", wip_limit: null },
  { key: "ready", title: "Ready", wip_limit: null },
  { key: "in_progress", title: "In Progress", wip_limit: null },
  { key: "blocked", title: "Blocked", wip_limit: null },
  { key: "review", title: "Review", wip_limit: null },
  { key: "done", title: "Done", wip_limit: null },
];

function buildSummary(board, cards) {
  const cardsByColumn = Object.fromEntries(
    columns.map((column) => [column.key, 0]),
  );

  cards.forEach((card) => {
    cardsByColumn[card.column_key] = (cardsByColumn[card.column_key] ?? 0) + 1;
  });

  return {
    card_count: cards.length,
    cards_by_column: cardsByColumn,
    open_commitment_count: 0,
    document_count: 0,
    latest_activity_at: board.updated_at,
    has_primary_document: Boolean(board.primary_document_id),
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

    return String(left.thread_id).localeCompare(String(right.thread_id));
  });
}

function buildWorkspace(board, cards, threads, documents) {
  const sortedCards = sortCards(cards);

  return {
    board_id: board.id,
    board,
    primary_thread: threads.find(
      (thread) => thread.id === board.primary_thread_id,
    ),
    primary_document:
      documents.find((document) => document.id === board.primary_document_id) ??
      null,
    cards: {
      items: sortedCards.map((card) => ({
        card,
        thread: threads.find((thread) => thread.id === card.thread_id),
        summary: {
          open_commitment_count: 0,
          decision_request_count: 0,
          decision_count: 0,
          recommendation_count: 0,
          document_count: 0,
          inbox_count: 0,
          latest_activity_at: card.updated_at,
          stale: false,
        },
        pinned_document:
          documents.find(
            (document) => document.id === card.pinned_document_id,
          ) ?? null,
      })),
      count: cards.length,
    },
    documents: {
      items: [],
      count: 0,
    },
    commitments: {
      items: [],
      count: 0,
    },
    inbox: {
      items: [],
      count: 0,
      generated_at: board.updated_at,
    },
    board_summary: buildSummary(board, cards),
    warnings: {
      items: [],
      count: 0,
    },
    section_kinds: {
      board: "canonical",
      primary_thread: "derived",
      primary_document: "derived",
      cards: "canonical",
      documents: "derived",
      commitments: "derived",
      inbox: "derived",
      board_summary: "derived",
    },
    generated_at: board.updated_at,
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
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      open_commitments: [],
    },
    {
      id: "thread-execution",
      type: "process",
      title: "Execution Track",
      status: "active",
      priority: "p2",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      open_commitments: [],
    },
    {
      id: "thread-review",
      type: "process",
      title: "Review Prep",
      status: "active",
      priority: "p2",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      open_commitments: [],
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
    {
      id: "doc-playbook",
      title: "Incident Playbook",
      updated_at: "2026-03-04T00:00:00.000Z",
      updated_by: actorId,
      status: "active",
      labels: [],
      head_revision_id: "rev-doc-playbook-1",
      head_revision_number: 1,
    },
  ];

  let board = null;
  let cards = [];
  let mutationCounter = 0;
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
          boards: board ? [{ board, summary: buildSummary(board, cards) }] : [],
        }),
      });
      return;
    }

    const payload = JSON.parse(request.postData() ?? "{}");
    board = {
      id: "board-created",
      title: payload.board.title,
      status: payload.board.status,
      labels: payload.board.labels ?? [],
      owners: payload.board.owners ?? [actorId],
      primary_thread_id: payload.board.primary_thread_id,
      primary_document_id: payload.board.primary_document_id ?? null,
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
      body: JSON.stringify(buildWorkspace(board, cards, threads, documents)),
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
    const targetColumnCards = cards.filter(
      (card) => card.column_key === payload.column_key,
    );
    const newCard = {
      board_id: "board-created",
      thread_id: payload.thread_id,
      column_key: payload.column_key,
      rank: String(targetColumnCards.length + 1).padStart(4, "0"),
      pinned_document_id: payload.pinned_document_id ?? null,
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

      const movingCard = cards.find((card) => card.thread_id === cardId);
      const groupedCards = Object.fromEntries(
        columns.map((column) => [column.key, []]),
      );

      cards
        .filter((card) => card.thread_id !== cardId)
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
      const targetCards = groupedCards[payload.column_key];
      let insertIndex = targetCards.length;
      if (payload.before_thread_id) {
        insertIndex = targetCards.findIndex(
          (card) => card.thread_id === payload.before_thread_id,
        );
      } else if (payload.after_thread_id) {
        const afterIndex = targetCards.findIndex(
          (card) => card.thread_id === payload.after_thread_id,
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

  await page.route(
    /\/boards\/board-created\/cards\/thread-execution$/,
    async (route) => {
      const payload = JSON.parse(route.request().postData() ?? "{}");
      updateCardPayloads.push(payload);
      const card = cards.find((item) => item.thread_id === "thread-execution");
      card.pinned_document_id = payload.patch.pinned_document_id;
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
    },
  );

  await page.route(
    /\/boards\/board-created\/cards\/thread-execution\/remove$/,
    async (route) => {
      const payload = JSON.parse(route.request().postData() ?? "{}");
      removePayloads.push(payload);
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
        body: JSON.stringify({ board, removed_thread_id: "thread-execution" }),
      });
    },
  );

  await page.goto("/local/boards");

  await expect(page.getByRole("heading", { name: "Boards" })).toBeVisible();
  await page.getByRole("button", { name: "Create board", exact: true }).click();
  await page.getByLabel("Board title").fill("Launch Control");
  await page.getByLabel("Primary thread ID").fill("thread-primary");
  await page.getByLabel("Primary document ID").fill("doc-runbook");
  await page.getByRole("button", { name: "Create board", exact: true }).click();

  await expect(page).toHaveURL(/\/local\/boards\/board-created$/);
  await expect(
    page.getByRole("heading", { name: "Launch Control" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Edit board" }).click();
  await page.getByLabel("Board title").fill("Launch Control v2");
  await page.getByLabel("Primary document ID").fill("doc-playbook");
  await page.getByRole("button", { name: "Save board" }).click();

  await expect(
    page.getByRole("heading", { name: "Launch Control v2" }),
  ).toBeVisible();
  expect(boardPatchPayloads).toEqual([
    {
      actor_id: actorId,
      if_updated_at: "2026-03-05T00:00:00.000Z",
      patch: {
        title: "Launch Control v2",
        status: "active",
        primary_document_id: "doc-playbook",
        labels: [],
        owners: [actorId],
        pinned_refs: [],
      },
    },
  ]);

  await page.getByRole("button", { name: "Add card", exact: true }).click();
  await page.getByLabel("Thread ID").fill("thread-execution");
  await page.getByLabel("Target column").selectOption("ready");
  await page.getByLabel("Pinned document ID").fill("doc-playbook");
  await page.getByRole("button", { name: "Add card", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Execution Track" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Add card", exact: true }).click();
  await page.getByLabel("Thread ID").fill("thread-review");
  await page.getByLabel("Target column").selectOption("ready");
  await page.getByRole("button", { name: "Add card", exact: true }).click();
  await expect(page.getByRole("link", { name: "Review Prep" })).toBeVisible();

  await page.getByRole("button", { name: "Manage Review Prep" }).click();
  await page.getByRole("button", { name: "Move up" }).click();

  const readySection = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "Ready" }) });
  await expect(
    readySection.locator('a[href*="/threads/"]').nth(0),
  ).toContainText("Review Prep");
  await expect(
    readySection.locator('a[href*="/threads/"]').nth(1),
  ).toContainText("Execution Track");

  await page.getByRole("button", { name: "Manage Review Prep" }).click();
  await page.getByLabel("Move to column").selectOption("review");
  await page.getByRole("button", { name: "Move to column" }).click();

  const reviewSection = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "Review" }) });
  await expect(
    reviewSection.getByRole("link", { name: "Review Prep" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Manage Execution Track" }).click();
  await page.getByLabel("Pinned document ID").fill("doc-playbook");
  await page.getByRole("button", { name: "Save pinned doc" }).click();
  await expect(
    readySection.getByRole("link", { name: "Incident Playbook" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Manage Execution Track" }).click();
  await page.getByRole("button", { name: "Remove card" }).click();
  await expect(
    readySection.getByRole("link", { name: "Execution Track" }),
  ).toHaveCount(0);

  expect(addCardPayloads).toEqual([
    {
      actor_id: actorId,
      if_board_updated_at: "2026-03-05T01:00:00.000Z",
      thread_id: "thread-execution",
      column_key: "ready",
      pinned_document_id: "doc-playbook",
    },
    {
      actor_id: actorId,
      if_board_updated_at: "2026-03-05T02:00:00.000Z",
      thread_id: "thread-review",
      column_key: "ready",
    },
  ]);
  expect(movePayloads).toEqual([
    {
      cardId: "thread-review",
      payload: {
        actor_id: actorId,
        if_board_updated_at: "2026-03-05T03:00:00.000Z",
        column_key: "ready",
        before_thread_id: "thread-execution",
      },
    },
    {
      cardId: "thread-review",
      payload: {
        actor_id: actorId,
        if_board_updated_at: "2026-03-05T04:00:00.000Z",
        column_key: "review",
      },
    },
  ]);
  expect(updateCardPayloads).toEqual([
    {
      actor_id: actorId,
      if_board_updated_at: "2026-03-05T05:00:00.000Z",
      patch: {
        pinned_document_id: "doc-playbook",
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
      open_commitments: [],
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
    primary_thread_id: "thread-primary",
    primary_document_id: "doc-runbook",
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
      body: JSON.stringify(buildWorkspace(board, [], threads, documents)),
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

  await page.getByRole("button", { name: "Edit board" }).click();
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
        primary_document_id: "doc-runbook",
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
        primary_document_id: "doc-runbook",
        labels: [],
        owners: [actorId],
        pinned_refs: [],
      },
    },
  ]);
});
