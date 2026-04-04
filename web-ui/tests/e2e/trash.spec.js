import { expect, test } from "@playwright/test";

test("trash page restores archived topics, boards, cards, documents, and artifacts", async ({
  page,
}) => {
  const actorId = "actor-trash-e2e";
  let artifacts = [
    {
      id: "artifact-trash-1",
      kind: "work_order",
      summary: "Archived work order",
      thread_id: "topic-trash-1",
      created_at: "2026-03-01T08:00:00.000Z",
      created_by: actorId,
      tombstoned_at: "2026-03-05T08:00:00.000Z",
      tombstoned_by: actorId,
      tombstone_reason: "Seeded trash sample",
    },
  ];
  let documents = [
    {
      id: "doc-trash-1",
      title: "Archived document",
      thread_id: "topic-trash-1",
      status: "active",
      labels: [],
      created_at: "2026-03-01T08:00:00.000Z",
      created_by: actorId,
      updated_at: "2026-03-01T08:00:00.000Z",
      updated_by: actorId,
      tombstoned_at: "2026-03-05T08:00:00.000Z",
      tombstoned_by: actorId,
      tombstone_reason: "Seeded trash sample",
    },
  ];
  let topics = [
    {
      id: "topic-trash-1",
      title: "Archived topic",
      summary: "Archived topic summary",
      type: "initiative",
      status: "archived",
      thread_id: "topic-trash-1",
      owner_refs: [],
      board_refs: [],
      document_refs: [],
      related_refs: [],
      created_at: "2026-03-01T08:00:00.000Z",
      created_by: actorId,
      updated_at: "2026-03-01T08:00:00.000Z",
      updated_by: actorId,
      tombstoned_at: "2026-03-05T08:00:00.000Z",
      tombstoned_by: actorId,
      tombstone_reason: "Seeded trash sample",
    },
  ];
  let boards = [
    {
      board: {
        id: "board-trash-1",
        title: "Archived board",
        status: "archived",
        labels: [],
        owners: [actorId],
        thread_id: "topic-trash-1",
        refs: [
          "document:doc-trash-1",
          "thread:topic-trash-1",
          "topic:topic-trash-1",
        ],
        document_refs: ["document:doc-trash-1"],
        card_refs: ["card:card-trash-1"],
        pinned_refs: [],
        created_at: "2026-03-01T08:00:00.000Z",
        created_by: actorId,
        updated_at: "2026-03-05T08:00:00.000Z",
        updated_by: actorId,
        archived_at: "2026-03-05T08:00:00.000Z",
        archived_by: actorId,
        tombstoned_at: "2026-03-05T08:00:00.000Z",
        tombstoned_by: actorId,
        tombstone_reason: "Seeded trash sample",
      },
      summary: {
        card_count: 1,
        cards_by_column: {
          backlog: 0,
          ready: 0,
          in_progress: 0,
          blocked: 0,
          review: 0,
          done: 1,
        },
        latest_activity_at: "2026-03-05T08:00:00.000Z",
        has_document_ref: true,
      },
    },
  ];
  let cards = [
    {
      id: "card-trash-1",
      board_id: "board-trash-1",
      board_ref: "board:board-trash-1",
      topic_ref: "topic:topic-trash-1",
      thread_id: "topic-trash-1",
      document_ref: "document:doc-trash-1",
      title: "Archived card",
      summary: "Archived card summary",
      column_key: "done",
      rank: "0001",
      assignee_refs: [],
      risk: "medium",
      resolution: "done",
      resolution_refs: ["artifact:artifact-trash-1"],
      related_refs: ["topic:topic-trash-1"],
      created_at: "2026-03-01T08:00:00.000Z",
      created_by: actorId,
      updated_at: "2026-03-05T08:00:00.000Z",
      updated_by: actorId,
      archived_at: "2026-03-05T08:00:00.000Z",
      archived_by: actorId,
      tombstoned_at: "2026-03-05T08:00:00.000Z",
      tombstoned_by: actorId,
      tombstone_reason: "Seeded trash sample",
    },
  ];

  function restoreById(collection, id, patch) {
    const item = collection.find((entry) => String(entry.id ?? "") === id);
    if (!item) {
      return null;
    }

    Object.assign(item, patch, {
      archived_at: null,
      archived_by: null,
      tombstoned_at: null,
      tombstoned_by: null,
      tombstone_reason: null,
    });
    return item;
  }

  function restoreBoardById(boardId) {
    const wrapper = boards.find(
      (entry) => String(entry?.board?.id ?? "") === boardId,
    );
    if (!wrapper?.board) {
      return null;
    }

    Object.assign(wrapper.board, {
      status: "active",
      archived_at: null,
      archived_by: null,
      tombstoned_at: null,
      tombstoned_by: null,
      tombstone_reason: null,
    });
    return wrapper.board;
  }

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "Trash Tester", tags: ["human"] },
        ],
      }),
    });
  });

  await page.route(/\/artifacts(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        artifacts: artifacts.filter((artifact) => artifact.tombstoned_at),
      }),
    });
  });

  await page.route(/\/artifacts\/[^/?]+\/restore$/, async (route) => {
    const artifactId = route.request().url().split("/").at(-2) ?? "";
    const restored = restoreById(artifacts, artifactId, {});
    await route.fulfill({
      status: restored ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        restored ? { artifact: restored } : { error: "not found" },
      ),
    });
  });

  await page.route(/\/docs(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        documents: documents.filter((document) => document.tombstoned_at),
      }),
    });
  });

  await page.route(/\/docs\/[^/?]+\/restore$/, async (route) => {
    const documentId = route.request().url().split("/").at(-2) ?? "";
    const restored = restoreById(documents, documentId, { status: "active" });
    await route.fulfill({
      status: restored ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        restored ? { document: restored } : { error: "not found" },
      ),
    });
  });

  await page.route(/\/topics(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        topics: topics.filter(
          (topic) =>
            Boolean(topic.archived_at) ||
            Boolean(topic.tombstoned_at) ||
            String(topic.status ?? "").trim() === "archived",
        ),
      }),
    });
  });

  await page.route(/\/topics\/[^/?]+\/restore$/, async (route) => {
    const topicId = route.request().url().split("/").at(-2) ?? "";
    const restored = restoreById(topics, topicId, { status: "active" });
    await route.fulfill({
      status: restored ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        restored ? { topic: restored } : { error: "not found" },
      ),
    });
  });

  await page.route(/\/boards(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        boards: boards.filter((item) => item.board.tombstoned_at),
      }),
    });
  });

  await page.route(/\/boards\/[^/?]+\/restore$/, async (route) => {
    const boardId = route.request().url().split("/").at(-2) ?? "";
    const restored = restoreBoardById(boardId);
    await route.fulfill({
      status: restored ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        restored ? { board: restored } : { error: "not found" },
      ),
    });
  });

  await page.route(/\/cards(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        cards: cards.filter((card) => card.tombstoned_at),
      }),
    });
  });

  await page.route(/\/cards\/[^/?]+\/restore$/, async (route) => {
    const cardId = route.request().url().split("/").at(-2) ?? "";
    const restored = restoreById(cards, cardId, {});
    await route.fulfill({
      status: restored ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        restored ? { card: restored } : { error: "not found" },
      ),
    });
  });

  await page.route(/\/artifacts\/[^/?]+\/purge$/, async (route) => {
    const artifactId = route.request().url().split("/").at(-2) ?? "";
    const idx = artifacts.findIndex((a) => String(a.id) === artifactId);
    if (idx >= 0) {
      artifacts.splice(idx, 1);
    }
    await route.fulfill({
      status: idx >= 0 ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        idx >= 0 ? { artifact_id: artifactId } : { error: "not found" },
      ),
    });
  });

  await page.route(/\/docs\/[^/?]+\/purge$/, async (route) => {
    const documentId = route.request().url().split("/").at(-2) ?? "";
    const idx = documents.findIndex((d) => String(d.id) === documentId);
    if (idx >= 0) {
      documents.splice(idx, 1);
    }
    await route.fulfill({
      status: idx >= 0 ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        idx >= 0 ? { document_id: documentId } : { error: "not found" },
      ),
    });
  });

  await page.route(/\/boards\/[^/?]+\/purge$/, async (route) => {
    const boardId = route.request().url().split("/").at(-2) ?? "";
    const idx = boards.findIndex((b) => String(b?.board?.id ?? "") === boardId);
    if (idx >= 0) {
      boards.splice(idx, 1);
    }
    await route.fulfill({
      status: idx >= 0 ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        idx >= 0 ? { board_id: boardId } : { error: "not found" },
      ),
    });
  });

  await page.route(/\/cards\/[^/?]+\/purge$/, async (route) => {
    const cardId = route.request().url().split("/").at(-2) ?? "";
    const idx = cards.findIndex((c) => String(c.id) === cardId);
    if (idx >= 0) {
      cards.splice(idx, 1);
    }
    await route.fulfill({
      status: idx >= 0 ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(
        idx >= 0 ? { card_id: cardId } : { error: "not found" },
      ),
    });
  });

  await page.goto("/local/trash");

  await expect(page.getByRole("heading", { name: "Trash" })).toBeVisible();
  await expect(
    page.getByRole("tab", { name: /Artifacts \(1\)/ }),
  ).toBeVisible();
  await expect(
    page.getByRole("tab", { name: /Documents \(1\)/ }),
  ).toBeVisible();
  await expect(page.getByRole("tab", { name: /Topics \(1\)/ })).toBeVisible();
  await expect(page.getByRole("tab", { name: /Boards \(1\)/ })).toBeVisible();
  await expect(page.getByRole("tab", { name: /Cards \(1\)/ })).toBeVisible();

  await page.getByRole("tab", { name: /Topics/ }).click();
  await expect(page.getByText("Archived topic", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Restore" }).click();
  await expect(page.getByText("Archived topic", { exact: true })).toHaveCount(
    0,
  );

  await page.getByRole("tab", { name: /Boards/ }).click();
  await expect(page.getByText("Archived board")).toBeVisible();
  await page.getByRole("button", { name: "Restore" }).click();
  await expect(page.getByText("Archived board")).toHaveCount(0);

  await page.getByRole("tab", { name: /Cards/ }).click();
  await expect(page.getByText("Archived card", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Restore" }).click();
  await expect(page.getByText("Archived card", { exact: true })).toHaveCount(0);

  await page.getByRole("tab", { name: /Documents/ }).click();
  await expect(page.getByText("Archived document")).toBeVisible();
  await page.getByRole("button", { name: "Restore" }).click();
  await expect(page.getByText("Archived document")).toHaveCount(0);

  await page.getByRole("tab", { name: /Artifacts/ }).click();
  await expect(page.getByText("Archived work order")).toBeVisible();
  await page.getByRole("button", { name: "Restore" }).click();
  await expect(page.getByText("Archived work order")).toHaveCount(0);
});

test("trash page purges a card after confirmation (human principal)", async ({
  page,
}) => {
  const actorId = "actor-trash-purge-card";
  const cardId = "card-purge-e2e";
  let cards = [
    {
      id: cardId,
      board_id: "board-purge-e2e",
      board_ref: "board:board-purge-e2e",
      topic_ref: "topic:t-purge",
      thread_id: "t-purge",
      document_ref: null,
      title: "Card pending purge",
      summary: "",
      column_key: "done",
      rank: "a",
      assignee_refs: [],
      risk: "medium",
      resolution: "done",
      resolution_refs: [],
      related_refs: [],
      created_at: "2026-03-01T08:00:00.000Z",
      created_by: actorId,
      updated_at: "2026-03-05T08:00:00.000Z",
      updated_by: actorId,
      archived_at: "2026-03-05T08:00:00.000Z",
      archived_by: actorId,
      tombstoned_at: "2026-03-05T08:00:00.000Z",
      tombstoned_by: actorId,
      tombstone_reason: "e2e purge",
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
        actors: [
          { id: actorId, display_name: "Purge Tester", tags: ["human"] },
        ],
      }),
    });
  });

  const emptyListRoutes = [
    [/\/artifacts(\?.*)?$/, { artifacts: [] }],
    [/\/docs(\?.*)?$/, { documents: [] }],
    [/\/topics(\?.*)?$/, { topics: [] }],
    [/\/boards(\?.*)?$/, { boards: [] }],
  ];
  for (const [pattern, json] of emptyListRoutes) {
    await page.route(pattern, async (route) => {
      const request = route.request();
      if (request.method() !== "GET") {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(json),
      });
    });
  }

  await page.route(/\/cards(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() !== "GET") {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        cards: cards.filter(
          (card) =>
            Boolean(card?.archived_at) ||
            Boolean(card?.tombstoned_at) ||
            String(card?.status ?? "").trim() === "archived",
        ),
      }),
    });
  });

  await page.route(/\/cards\/[^/?]+\/purge$/, async (route) => {
    const id = route.request().url().split("/").at(-2) ?? "";
    const idx = cards.findIndex((c) => String(c.id) === id);
    if (idx >= 0) {
      cards.splice(idx, 1);
    }
    await route.fulfill({
      status: idx >= 0 ? 200 : 404,
      contentType: "application/json",
      body: JSON.stringify(idx >= 0 ? { card_id: id } : { error: "not found" }),
    });
  });

  await page.goto("/local/trash");
  await expect(page.getByRole("heading", { name: "Trash" })).toBeVisible();
  await page.getByRole("tab", { name: /Cards/ }).click();
  await expect(
    page.getByText("Card pending purge", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Purge" }).click();
  await page.getByRole("button", { name: "Confirm purge" }).click();
  await expect(
    page.getByText("Card pending purge", { exact: true }),
  ).toHaveCount(0);
});
