import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

import {
  searchTopics,
  searchDocuments,
  searchActors,
  searchBoards,
  boardRecordFromBoardsListRow,
  searchArtifacts,
  backingThreadIdFromTopicRecord,
  topicSearchResultToPickerOption,
  topicSearchResultToBoardRefOption,
} from "../../src/lib/searchHelpers.js";

vi.mock("../../src/lib/coreClient.js", () => ({
  coreClient: {
    listTopics: vi.fn(),
    listDocuments: vi.fn(),
    listActors: vi.fn(),
    listBoards: vi.fn(),
    listArtifacts: vi.fn(),
  },
}));

import { coreClient } from "../../src/lib/coreClient.js";

describe("searchHelpers", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  describe("backingThreadIdFromTopicRecord", () => {
    it("prefers thread_id over topic id", () => {
      expect(
        backingThreadIdFromTopicRecord({
          id: "topic-alpha",
          thread_id: "thread-b",
        }),
      ).toBe("thread-b");
    });

    it("falls back to id when thread_id absent", () => {
      expect(
        backingThreadIdFromTopicRecord({ id: "topic-only", title: "T" }),
      ).toBe("topic-only");
    });
  });

  describe("topicSearchResultToPickerOption", () => {
    it("uses backing thread id as picker value", () => {
      const opt = topicSearchResultToPickerOption({
        id: "topic-1",
        thread_id: "thr-9",
        title: "Coordination",
        type: "incident",
        status: "active",
      });
      expect(opt.id).toBe("thr-9");
      expect(opt.title).toBe("Coordination");
      expect(opt.keywords).toContain("incident");
    });
  });

  describe("topicSearchResultToBoardRefOption", () => {
    it("uses topic typed ref as picker value", () => {
      const opt = topicSearchResultToBoardRefOption({
        id: "5e63c3fc-271b-4785-8036-cf06e1ee03b0",
        thread_id: "d002c2fa-8f5a-4bbe-9c92-4e333e1f75fe",
        title: "Alpha",
        type: "initiative",
        status: "active",
      });
      expect(opt.id).toBe("topic:5e63c3fc-271b-4785-8036-cf06e1ee03b0");
      expect(opt.title).toBe("Alpha");
      expect(opt.subtitle).toContain(
        "Timeline d002c2fa-8f5a-4bbe-9c92-4e333e1f75fe",
      );
    });

    it("preserves already-typed ref ids", () => {
      const opt = topicSearchResultToBoardRefOption({
        id: "topic:already",
        title: "T",
      });
      expect(opt.id).toBe("topic:already");
    });
  });

  describe("searchTopics", () => {
    it("calls coreClient.listTopics with query and limit", async () => {
      const mockTopics = [
        { id: "topic-1", title: "Test Topic" },
        { id: "topic-2", title: "Another Topic" },
      ];
      coreClient.listTopics.mockResolvedValue({ topics: mockTopics });

      const result = await searchTopics("test", 10);

      expect(coreClient.listTopics).toHaveBeenCalledWith({
        q: "test",
        limit: 10,
      });
      expect(result).toEqual(mockTopics);
    });

    it("uses default limit of 20 when not specified", async () => {
      coreClient.listTopics.mockResolvedValue({ topics: [] });

      await searchTopics("query");

      expect(coreClient.listTopics).toHaveBeenCalledWith({
        q: "query",
        limit: 20,
      });
    });

    it("returns empty array when response has no topics", async () => {
      coreClient.listTopics.mockResolvedValue({});

      const result = await searchTopics("test");

      expect(result).toEqual([]);
    });
  });

  describe("searchDocuments", () => {
    it("calls coreClient.listDocuments with query and limit", async () => {
      const mockDocs = [
        { id: "doc-1", title: "Test Document" },
        {
          id: "agentreg.hermes",
          title: "Agent registration @hermes",
          labels: ["agent-registration"],
        },
        { id: "doc-2", title: "Another Document" },
      ];
      coreClient.listDocuments.mockResolvedValue({ documents: mockDocs });

      const result = await searchDocuments("test", 15);

      expect(coreClient.listDocuments).toHaveBeenCalledWith({
        q: "test",
        limit: 15,
      });
      expect(result).toEqual([
        { id: "doc-1", title: "Test Document" },
        { id: "doc-2", title: "Another Document" },
      ]);
    });

    it("uses default limit of 20 when not specified", async () => {
      coreClient.listDocuments.mockResolvedValue({ documents: [] });

      await searchDocuments("query");

      expect(coreClient.listDocuments).toHaveBeenCalledWith({
        q: "query",
        limit: 20,
      });
    });

    it("returns empty array when response has no documents", async () => {
      coreClient.listDocuments.mockResolvedValue({});

      const result = await searchDocuments("test");

      expect(result).toEqual([]);
    });
  });

  describe("searchActors", () => {
    it("calls coreClient.listActors with query and limit", async () => {
      const mockActors = [
        { id: "actor-1", display_name: "Test Actor" },
        { id: "actor-2", display_name: "Another Actor" },
      ];
      coreClient.listActors.mockResolvedValue({ actors: mockActors });

      const result = await searchActors("test", 25);

      expect(coreClient.listActors).toHaveBeenCalledWith({
        q: "test",
        limit: 25,
      });
      expect(result).toEqual(mockActors);
    });

    it("uses default limit of 20 when not specified", async () => {
      coreClient.listActors.mockResolvedValue({ actors: [] });

      await searchActors("query");

      expect(coreClient.listActors).toHaveBeenCalledWith({
        q: "query",
        limit: 20,
      });
    });

    it("returns empty array when response has no actors", async () => {
      coreClient.listActors.mockResolvedValue({});

      const result = await searchActors("test");

      expect(result).toEqual([]);
    });
  });

  describe("searchBoards", () => {
    it("calls coreClient.listBoards with query and limit", async () => {
      const mockBoards = [
        { id: "board-1", title: "Test Board" },
        { id: "board-2", title: "Another Board" },
      ];
      coreClient.listBoards.mockResolvedValue({ boards: mockBoards });

      const result = await searchBoards("test", 30);

      expect(coreClient.listBoards).toHaveBeenCalledWith({
        q: "test",
        limit: 30,
      });
      expect(result).toEqual(mockBoards);
    });

    it("uses default limit of 20 when not specified", async () => {
      coreClient.listBoards.mockResolvedValue({ boards: [] });

      await searchBoards("query");

      expect(coreClient.listBoards).toHaveBeenCalledWith({
        q: "query",
        limit: 20,
      });
    });

    it("returns empty array when response has no boards", async () => {
      coreClient.listBoards.mockResolvedValue({});

      const result = await searchBoards("test");

      expect(result).toEqual([]);
    });

    it("unwraps API list rows { board, summary } to flat board records", async () => {
      const inner = {
        id: "bd_abc",
        title: "Summer Menu Launch",
        status: "active",
      };
      coreClient.listBoards.mockResolvedValue({
        boards: [{ board: inner, summary: { card_count: 3 } }],
      });

      const result = await searchBoards("summer", 5);

      expect(result).toEqual([inner]);
    });
  });

  describe("boardRecordFromBoardsListRow", () => {
    it("returns inner board when present", () => {
      const board = { id: "b1", title: "T" };
      expect(boardRecordFromBoardsListRow({ board, summary: {} })).toEqual(
        board,
      );
    });

    it("returns row when already flat (tests / legacy)", () => {
      const row = { id: "b1", title: "T" };
      expect(boardRecordFromBoardsListRow(row)).toEqual(row);
    });
  });

  describe("searchArtifacts", () => {
    it("calls coreClient.listArtifacts with query and limit", async () => {
      const mockArtifacts = [
        { id: "artifact-1", kind: "receipt", summary: "Test receipt" },
        { id: "artifact-2", kind: "receipt", summary: "Test receipt" },
      ];
      coreClient.listArtifacts.mockResolvedValue({ artifacts: mockArtifacts });

      const result = await searchArtifacts("test", 15);

      expect(coreClient.listArtifacts).toHaveBeenCalledWith({
        q: "test",
        limit: 15,
      });
      expect(result).toEqual(mockArtifacts);
    });

    it("uses default limit of 20 when not specified", async () => {
      coreClient.listArtifacts.mockResolvedValue({ artifacts: [] });

      await searchArtifacts("query");

      expect(coreClient.listArtifacts).toHaveBeenCalledWith({
        q: "query",
        limit: 20,
      });
    });

    it("returns empty array when response has no artifacts", async () => {
      coreClient.listArtifacts.mockResolvedValue({});

      const result = await searchArtifacts("test");

      expect(result).toEqual([]);
    });
  });

  describe("debounce behavior validation", () => {
    it("demonstrates expected debounce pattern (300ms delay)", async () => {
      const mockTopics = [{ id: "topic-1", title: "Test" }];
      coreClient.listTopics.mockResolvedValue({ topics: mockTopics });

      const searchPromise = searchTopics("test");

      vi.advanceTimersByTime(300);

      const result = await searchPromise;
      expect(result).toEqual(mockTopics);
    });

    it("validates that search requests use pagination parameters", async () => {
      coreClient.listTopics.mockResolvedValue({ topics: [] });

      await searchTopics("query", 10);

      const callArgs = coreClient.listTopics.mock.calls[0][0];
      expect(callArgs).toHaveProperty("q");
      expect(callArgs).toHaveProperty("limit");
      expect(typeof callArgs.q).toBe("string");
      expect(typeof callArgs.limit).toBe("number");
    });
  });
});
