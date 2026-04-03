import { describe, expect, it } from "vitest";

describe("mockCoreData parity behaviors", () => {
  describe("inbox ack is non-destructive", () => {
    it("module exports ackMockInboxItem and listMockInboxItems functions", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      expect(typeof mod.ackMockInboxItem).toBe("function");
      expect(typeof mod.listMockInboxItems).toBe("function");
    });
  });

  describe("documents list matches contract behavior", () => {
    it("filters tombstoned docs by default and sorts by updated_at desc", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const docs = mod.listMockDocuments();

      expect(docs.map((doc) => doc.id)).toEqual([
        "product-constitution",
        "incident-response-playbook",
        "onboarding-guide-v1",
      ]);
    });

    it("includes tombstoned docs when requested", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const docs = mod.listMockDocuments({ include_tombstoned: true });

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

  describe("artifacts trash list (tombstoned_only)", () => {
    it("returns only tombstoned artifacts when tombstoned_only is true", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const trashed = mod.listMockArtifacts({ tombstoned_only: true });
      const ids = trashed.map((a) => a.id).sort();

      expect(ids).toContain("artifact-dev-trash-onboarding-draft");
      expect(ids).toContain("artifact-dev-trash-ops-scratch");
      expect(ids).toContain("artifact-tombstoned-doc");
      expect(trashed.every((a) => a.tombstoned_at != null)).toBe(true);
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
          pinned_document_id: "onboarding-guide-v1",
        },
      });
    });

    it("rejects invalid board status values on create", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const result = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          title: "Invalid",
          primary_thread_id: "thread-summer-menu",
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
          primary_thread_id: "thread-summer-menu",
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
          primary_thread_id: "thread-summer-menu",
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
        thread_id: "thread-pricing-glitch",
        pinned_document_id: "doc-does-not-exist",
      });

      expect(result).toMatchObject({
        error: "not_found",
        message: "Document not found: doc-does-not-exist",
      });
    });

    it("aggregates workspace documents and commitments across all board threads", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const workspace = mod.getMockBoardWorkspace("board-summer-menu");

      expect(workspace?.documents?.items?.map((doc) => doc.id)).toEqual([
        "incident-response-playbook",
        "onboarding-guide-v1",
      ]);
      expect(
        workspace?.commitments?.items?.map((commitment) => commitment.id),
      ).toEqual([
        "commitment-pricing-patch",
        "commitment-pricing-audit",
        "commitment-menu-board",
      ]);
      expect(workspace?.section_kinds).toMatchObject({
        board: "canonical",
        primary_thread: "canonical",
        primary_document: "canonical",
        cards: "convenience",
      });
    });

    it("includes primary-thread activity in board summaries", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const boardListItem = mod
        .listMockBoards()
        .find((item) => item.board.id === "board-supply-crisis");

      expect(boardListItem?.summary?.open_commitment_count).toBe(3);
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
        open_commitment_count: 0,
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
          primary_thread_id: "thread-summer-menu",
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

    it("applies board card patch fields in mock like core", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const { board } = mod.createMockBoard({
        actor_id: "actor-test",
        board: {
          id: "board-card-patch-test",
          title: "Patch test",
          primary_thread_id: "thread-summer-menu",
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
        body: "next",
        status: "in_progress",
        assignee: "actor-test",
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
          primary_thread_id: "thread-summer-menu",
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
          pinned_document_id: "doc-global-card-pin",
        },
      });
      expect(result.error).toBeUndefined();
      expect(result.card).toMatchObject({
        title: "Task A renamed",
        status: "done",
        pinned_document_id: "doc-global-card-pin",
        version: 2,
      });
    });
  });
});
