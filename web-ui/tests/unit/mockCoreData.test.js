import { describe, expect, it } from "vitest";

function assertTypedRef(refValue) {
  const value = String(refValue ?? "").trim();
  const separator = value.indexOf(":");
  expect(separator).toBeGreaterThan(0);
  expect(separator).toBeLessThan(value.length - 1);
}

describe("mockCoreData parity behaviors", () => {
  describe("inbox ack is non-destructive", () => {
    it("module exports ackMockInboxItem and listMockInboxItems functions", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      expect(typeof mod.ackMockInboxItem).toBe("function");
      expect(typeof mod.listMockInboxItems).toBe("function");
    });
  });

  describe("canonical seed data", () => {
    it("exposes topic, board, card, and packet seed views", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const seed = mod.getMockSeedData();

      expect(seed.topics[0]).toMatchObject({
        id: "thread-lemon-shortage",
        thread_id: "thread-lemon-shortage",
        type: "incident",
        status: "active",
      });
      expect(seed.boards[0]).toMatchObject({
        id: "board-product-launch",
        thread_id: "thread-q2-initiative",
      });
      expect(seed.cards[0]).toMatchObject({
        board_id: "board-product-launch",
        thread_id: "thread-summer-menu",
        topic_ref: "topic:thread-summer-menu",
        resolution: null,
      });
      expect(seed.cards[0].thread_ref).toBeUndefined();
      expect(
        seed.packets.every((packet) => packet?.artifact && packet?.packet),
      ).toBe(true);
      expect(seed.packets.map((packet) => packet.kind)).toEqual([
        "receipt",
        "review",
        "receipt",
        "review",
        "receipt",
        "review",
      ]);
    });

    it("normalizes topic related_refs into typed refs", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const seed = mod.getMockSeedData();

      seed.topics.forEach((topic) => {
        (topic.related_refs ?? []).forEach(assertTypedRef);
      });
      expect(
        mod.getMockTopic("thread-lemon-shortage")?.related_refs ?? [],
      ).toContain("artifact:artifact-supplier-sla");
    });

    it("keeps listed mock topic related_refs typed", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");

      mod.listMockTopics().forEach((topic) => {
        (topic.related_refs ?? []).forEach(assertTypedRef);
      });
    });

    it("keeps decision events topic-scoped for migrated contracts", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const seed = mod.getMockSeedData();
      const eventIds = new Set(["evt-price-003", "evt-price-008"]);

      const migratedEvents = seed.events.filter((event) =>
        eventIds.has(event.id),
      );
      expect(migratedEvents).toHaveLength(2);
      migratedEvents.forEach((event) => {
        expect(event.refs).toContain("topic:thread-pricing-glitch");
      });
    });
  });

  describe("documents list matches contract behavior", () => {
    it("filters trashed docs by default and sorts by updated_at desc", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const docs = mod.listMockDocuments();

      expect(docs.map((doc) => doc.id)).toEqual([
        "product-constitution",
        "incident-response-playbook",
        "onboarding-guide-v1",
      ]);
    });

    it("includes trashed docs when requested", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const docs = mod.listMockDocuments({ include_trashed: true });

      expect(docs.map((doc) => doc.id)).toEqual([
        "product-constitution",
        "incident-response-playbook",
        "old-pricing-doc",
        "onboarding-guide-v1",
      ]);
    });

    it("supports thread-scoped filtering and head revision summaries", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const docs = mod.listMockDocuments({ thread_id: "thread-q2-initiative" });

      expect(docs.map((doc) => doc.id)).toEqual(["product-constitution"]);
      expect(docs[0]?.head_revision).toMatchObject({
        revision_id: "rev-pc-3",
        revision_number: 3,
        content_type: "text",
      });
    });
  });

  describe("artifacts trash list (trashed_only)", () => {
    it("returns only trashed artifacts when trashed_only is true", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const trashed = mod.listMockArtifacts({ trashed_only: true });
      const ids = trashed.map((a) => a.id).sort();

      expect(ids).toContain("artifact-dev-trash-onboarding-draft");
      expect(ids).toContain("artifact-dev-trash-ops-scratch");
      expect(ids).toContain("artifact-trashed-doc");
      expect(trashed.every((a) => a.trashed_at != null)).toBe(true);
    });
  });

  describe("boards parity behaviors", () => {
    it("returns nested board memberships in thread workspace", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const workspace = mod.getMockThreadWorkspace("thread-summer-menu");
      const membership = workspace?.board_memberships?.items?.[0];

      expect(membership).toMatchObject({
        board: {
          id: "board-product-launch",
          title: "Q2 Product Launch",
          status: "active",
        },
        card: {
          board_id: "board-product-launch",
          thread_id: "thread-summer-menu",
          column_key: "ready",
          document_ref: "document:onboarding-guide-v1",
        },
      });
    });

    it("rejects invalid board status values on create", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const result = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          title: "Invalid",
          thread_id: "thread-summer-menu",
          status: "archived",
        },
      });

      expect(result).toMatchObject({
        error: "validation",
        message: "board.status must be one of: active, paused, closed",
      });
    });

    it("defaults omitted board owners to an empty list on create", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const result = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          id: "board-owner-default-test",
          title: "Owner Default",
          thread_id: "thread-summer-menu",
        },
      });

      expect(result?.board?.owners).toEqual([]);
    });

    it("rejects empty board titles on create", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const result = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          title: "   ",
          thread_id: "thread-summer-menu",
        },
      });

      expect(result).toMatchObject({
        error: "validation",
        message: "board.title is required",
      });
    });

    it("rejects invalid board columns on card creation", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const result = mod.createMockBoardCard("board-product-launch", {
        actor_id: "actor-test",
        if_board_updated_at: mod.getMockBoard("board-product-launch")
          ?.updated_at,
        title: "Invalid column card",
        thread_id: "thread-pricing-glitch",
        column_key: "triage",
      });

      expect(result).toMatchObject({
        error: "validation",
        message:
          "column_key must be one of: backlog, ready, in_progress, blocked, review, done.",
      });
    });

    it("rejects missing pinned documents on card creation", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const result = mod.createMockBoardCard("board-product-launch", {
        actor_id: "actor-test",
        if_board_updated_at: mod.getMockBoard("board-product-launch")
          ?.updated_at,
        title: "Missing doc ref card",
        thread_id: "thread-pricing-glitch",
        document_ref: "document:doc-does-not-exist",
      });

      expect(result).toMatchObject({
        error: "not_found",
        message: "Document not found: doc-does-not-exist",
      });
    });

    it("aggregates workspace documents and cards across all board threads", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const workspace = mod.getMockBoardWorkspace("board-summer-menu");

      expect(workspace?.documents?.items?.map((doc) => doc.id)).toEqual([
        "incident-response-playbook",
        "onboarding-guide-v1",
      ]);
      const threadIds = new Set(
        workspace?.cards?.items
          ?.map((item) => item.membership?.thread_id)
          .filter(Boolean) ?? [],
      );
      expect(threadIds.has("thread-onboarding")).toBe(true);
      expect(threadIds.has("thread-pricing-glitch")).toBe(true);
      expect(workspace?.section_kinds).toMatchObject({
        board: "canonical",
        cards: "convenience",
      });
    });

    it("includes backing-thread activity in board summaries", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const boardListItem = mod
        .listMockBoards()
        .find((item) => item.board.id === "board-supply-crisis");

      expect(boardListItem?.summary?.open_card_count).toBe(3);
      expect(
        Date.parse(String(boardListItem?.summary?.latest_activity_at ?? "")),
      ).toBeGreaterThan(
        Date.parse(String(boardListItem?.board?.updated_at ?? "")),
      );
    });

    it("uses derived collaboration and inbox counts in board card summaries", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const workspace = mod.getMockBoardWorkspace("board-summer-menu");
      const pricingCard = workspace?.cards?.items?.find(
        (item) => item.membership?.thread_id === "thread-pricing-glitch",
      );

      expect(pricingCard?.derived?.summary).toMatchObject({
        open_card_count: 0,
        decision_request_count: 1,
        decision_count: 1,
        recommendation_count: 0,
        document_count: 1,
        inbox_count: 0,
        stale: false,
      });
      expect(pricingCard?.derived?.freshness?.status).toBe("current");
    });

    it("rejects empty board titles on update", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const board = mod.getMockBoard("board-product-launch");
      const result = mod.updateMockBoard("board-product-launch", {
        actor_id: "actor-test",
        if_updated_at: board?.updated_at,
        patch: { title: "   " },
      });

      expect(result).toMatchObject({
        error: "validation",
        message: "board.title is required",
      });
    });

    it("renormalizes column ranks in board sort order after remove", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const { board } = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          id: "board-rank-remove-test",
          title: "Rank remove",
          thread_id: "thread-summer-menu",
        },
      });
      const boardId = board.id;
      let b = mod.getMockBoard(boardId);
      mod.createMockBoardCard(boardId, {
        actor_id: "actor-test",
        if_board_updated_at: b?.updated_at,
        title: "First",
        column_key: "backlog",
      });
      b = mod.getMockBoard(boardId);
      mod.createMockBoardCard(boardId, {
        actor_id: "actor-test",
        if_board_updated_at: b?.updated_at,
        title: "Second",
        column_key: "backlog",
      });
      const colCards = mod
        .listMockBoardCards(boardId)
        .filter((c) => c.column_key === "backlog");
      expect(colCards.length).toBe(2);
      const toRemoveId = colCards[0].id;
      b = mod.getMockBoard(boardId);
      mod.removeMockBoardCard(boardId, toRemoveId, {
        actor_id: "actor-test",
        if_board_updated_at: b?.updated_at,
      });
      const after = mod
        .listMockBoardCards(boardId)
        .filter((c) => c.column_key === "backlog");
      expect(after.length).toBe(1);
      expect(after[0].rank).toBe("0001");
    });

    it("archives cards without exposing them in active board views", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const { board } = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          id: "board-card-archive-test",
          title: "Archive test",
          thread_id: "thread-summer-menu",
        },
      });
      const boardId = board.id;
      let currentBoard = mod.getMockBoard(boardId);
      const { card } = mod.createMockBoardCard(boardId, {
        actor_id: "actor-test",
        if_board_updated_at: currentBoard?.updated_at,
        title: "Archive me",
        column_key: "ready",
      });

      const archiveResult = mod.archiveMockBoardCardByCardId(card.id, {
        actor_id: "actor-test",
      });
      expect(archiveResult.card.archived_at).toBeTruthy();
      expect(archiveResult.card.archived_by).toBe("actor-test");
      expect(mod.listMockBoardCards(boardId)).toHaveLength(0);
      expect(mod.getMockBoard(boardId)).toMatchObject({
        id: boardId,
      });
      expect(mod.getMockBoardWorkspace(boardId)?.cards?.count).toBe(0);
      expect(mod.getMockCard(card.id)).toMatchObject({
        id: card.id,
        archived_at: archiveResult.card.archived_at,
      });
    });

    it("restores and purges archived cards through lifecycle helpers", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const { board } = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          id: "board-card-restore-test",
          title: "Restore test",
          thread_id: "thread-summer-menu",
        },
      });
      const boardId = board.id;
      let currentBoard = mod.getMockBoard(boardId);
      const { card } = mod.createMockBoardCard(boardId, {
        actor_id: "actor-test",
        if_board_updated_at: currentBoard?.updated_at,
        title: "Restore me",
        column_key: "backlog",
      });

      mod.archiveMockBoardCardByCardId(card.id, {
        actor_id: "actor-test",
      });
      expect(mod.listMockBoardCards(boardId)).toHaveLength(0);

      const restoreResult = mod.restoreMockBoardCardByCardId(card.id, {
        actor_id: "actor-test",
      });
      expect(restoreResult.card.archived_at).toBeNull();
      expect(mod.listMockBoardCards(boardId)).toHaveLength(1);

      const purgeResult = mod.purgeMockBoardCardByCardId(card.id, {
        actor_id: "actor-test",
      });
      expect(purgeResult.card.id).toBe(card.id);
      expect(mod.getMockCard(card.id)).toBeNull();
      expect(mod.listMockBoardCards(boardId)).toHaveLength(0);
    });

    it("applies board card patch fields in mock like core", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const { board } = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          id: "board-card-patch-test",
          title: "Patch test",
          thread_id: "thread-summer-menu",
        },
      });
      const boardId = board.id;
      let b = mod.getMockBoard(boardId);
      const { card } = mod.createMockBoardCard(boardId, {
        actor_id: "actor-test",
        if_board_updated_at: b?.updated_at,
        title: "Alpha",
        body: "orig",
        column_key: "backlog",
      });
      b = mod.getMockBoard(boardId);
      const result = mod.updateMockBoardCard(boardId, card.id, {
        actor_id: "actor-test",
        if_board_updated_at: b?.updated_at,
        patch: {
          title: "Beta",
          body: "next",
          status: "in_progress",
          assignee: "actor-test",
        },
      });
      expect(result.error).toBeUndefined();
      expect(result.card).toMatchObject({
        title: "Beta",
        summary: "Beta",
        body: "next",
        status: "in_progress",
        assignee: "actor-test",
        assignee_refs: ["actor:actor-test"],
        version: 2,
      });
    });

    it("updates board card via global card id with title and status patch", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const { board } = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          id: "board-global-card-patch",
          title: "Global patch board",
          thread_id: "thread-summer-menu",
        },
      });
      const boardId = board.id;
      let b = mod.getMockBoard(boardId);
      const { card } = mod.createMockBoardCard(boardId, {
        actor_id: "actor-test",
        if_board_updated_at: b?.updated_at,
        title: "Task A",
        column_key: "backlog",
      });
      const globalId = card.id;
      b = mod.getMockBoard(boardId);
      mod.createMockDocument({
        actor_id: "actor-test",
        document: {
          id: "doc-global-card-pin",
          thread_id: "thread-summer-menu",
          title: "Pin for global card",
        },
        content: "# Pin",
        content_type: "text",
      });
      b = mod.getMockBoard(boardId);
      const result = mod.updateMockBoardCardByGlobalCardId(globalId, {
        actor_id: "actor-test",
        if_board_updated_at: b?.updated_at,
        patch: {
          title: "Task A renamed",
          status: "done",
          document_ref: "document:doc-global-card-pin",
        },
      });
      expect(result.error).toBeUndefined();
      expect(result.card).toMatchObject({
        title: "Task A renamed",
        summary: "Task A renamed",
        status: "done",
        document_ref: "document:doc-global-card-pin",
        version: 2,
      });
    });
  });
});
