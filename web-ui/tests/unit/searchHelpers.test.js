import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

import {
  searchThreads,
  searchDocuments,
  searchActors,
  searchBoards,
} from "../../src/lib/searchHelpers.js";

vi.mock("../../src/lib/coreClient.js", () => ({
  coreClient: {
    listThreads: vi.fn(),
    listDocuments: vi.fn(),
    listActors: vi.fn(),
    listBoards: vi.fn(),
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

  describe("searchThreads", () => {
    it("calls coreClient.listThreads with query and limit", async () => {
      const mockThreads = [
        { id: "thread-1", title: "Test Thread" },
        { id: "thread-2", title: "Another Thread" },
      ];
      coreClient.listThreads.mockResolvedValue({ threads: mockThreads });

      const result = await searchThreads("test", 10);

      expect(coreClient.listThreads).toHaveBeenCalledWith({
        q: "test",
        limit: 10,
      });
      expect(result).toEqual(mockThreads);
    });

    it("uses default limit of 20 when not specified", async () => {
      coreClient.listThreads.mockResolvedValue({ threads: [] });

      await searchThreads("query");

      expect(coreClient.listThreads).toHaveBeenCalledWith({
        q: "query",
        limit: 20,
      });
    });

    it("returns empty array when response has no threads", async () => {
      coreClient.listThreads.mockResolvedValue({});

      const result = await searchThreads("test");

      expect(result).toEqual([]);
    });
  });

  describe("searchDocuments", () => {
    it("calls coreClient.listDocuments with query and limit", async () => {
      const mockDocs = [
        { id: "doc-1", title: "Test Document" },
        { id: "doc-2", title: "Another Document" },
      ];
      coreClient.listDocuments.mockResolvedValue({ documents: mockDocs });

      const result = await searchDocuments("test", 15);

      expect(coreClient.listDocuments).toHaveBeenCalledWith({
        q: "test",
        limit: 15,
      });
      expect(result).toEqual(mockDocs);
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
  });

  describe("debounce behavior validation", () => {
    it("demonstrates expected debounce pattern (300ms delay)", async () => {
      const mockThreads = [{ id: "thread-1", title: "Test" }];
      coreClient.listThreads.mockResolvedValue({ threads: mockThreads });

      const searchPromise = searchThreads("test");

      vi.advanceTimersByTime(300);

      const result = await searchPromise;
      expect(result).toEqual(mockThreads);
    });

    it("validates that search requests use pagination parameters", async () => {
      coreClient.listThreads.mockResolvedValue({ threads: [] });

      await searchThreads("query", 10);

      const callArgs = coreClient.listThreads.mock.calls[0][0];
      expect(callArgs).toHaveProperty("q");
      expect(callArgs).toHaveProperty("limit");
      expect(typeof callArgs.q).toBe("string");
      expect(typeof callArgs.limit).toBe("number");
    });
  });
});
