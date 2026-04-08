import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const coreClientMocks = vi.hoisted(() => ({
  getThreadWorkspace: vi.fn(),
  getTopicWorkspace: vi.fn(),
  listThreadTimeline: vi.fn(),
  listTopicTimeline: vi.fn(),
}));

vi.mock("../../src/lib/coreClient.js", () => ({
  coreClient: {
    getThreadWorkspace: coreClientMocks.getThreadWorkspace,
    getTopicWorkspace: coreClientMocks.getTopicWorkspace,
    listThreadTimeline: coreClientMocks.listThreadTimeline,
    listTopicTimeline: coreClientMocks.listTopicTimeline,
  },
}));

import { topicDetailStore } from "../../src/lib/topicDetailStore.js";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("topicDetailStore", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    topicDetailStore.reset();
  });

  afterEach(() => {
    topicDetailStore.reset();
  });

  it("keeps existing workspace content mounted during background refresh", async () => {
    coreClientMocks.getThreadWorkspace.mockResolvedValueOnce({
      thread: { id: "thread-1", title: "Initial workspace" },
      context: {
        recent_events: [{ id: "event-seed", type: "actor_statement" }],
        documents: [],
        open_cards: [],
      },
    });

    await topicDetailStore.loadWorkspace("thread-1");

    expect(get(topicDetailStore).timeline).toEqual([
      { id: "event-seed", type: "actor_statement" },
    ]);
    expect(get(topicDetailStore).timelineThreadId).toBe("thread-1");

    coreClientMocks.listThreadTimeline.mockResolvedValueOnce({
      events: [{ id: "event-full", type: "message_posted" }],
    });
    await topicDetailStore.loadTimeline("thread-1");

    const pendingRefresh = deferred();
    coreClientMocks.getThreadWorkspace.mockReturnValueOnce(
      pendingRefresh.promise,
    );

    const refreshPromise = topicDetailStore.refreshTopicDetail("thread-1", {
      workspace: true,
    });

    expect(get(topicDetailStore).topicLoading).toBe(false);
    expect(get(topicDetailStore).topic).toMatchObject({
      id: "thread-1",
      title: "Initial workspace",
    });

    pendingRefresh.resolve({
      thread: { id: "thread-1", title: "Refreshed workspace" },
      context: {
        recent_events: [{ id: "event-1", type: "actor_statement" }],
        documents: [{ id: "doc-1", title: "Doc 1" }],
        open_cards: [{ id: "card-1", title: "Example" }],
      },
    });

    await refreshPromise;

    expect(get(topicDetailStore)).toMatchObject({
      topic: { id: "thread-1", title: "Refreshed workspace" },
      timelineThreadId: "thread-1",
      timeline: [{ id: "event-full", type: "message_posted" }],
      documents: [{ id: "doc-1", title: "Doc 1" }],
      topicLoading: false,
      topicError: "",
      documentsError: "",
    });
  });

  it("keeps the mounted timeline when a refresh fails", async () => {
    coreClientMocks.listThreadTimeline.mockResolvedValueOnce({
      events: [{ id: "event-1", type: "message_posted" }],
    });

    await topicDetailStore.loadTimeline("thread-1");

    coreClientMocks.listThreadTimeline.mockRejectedValueOnce(
      new Error("network down"),
    );

    await topicDetailStore.loadTimeline("thread-1");

    expect(get(topicDetailStore)).toMatchObject({
      timelineThreadId: "thread-1",
      timeline: [{ id: "event-1", type: "message_posted" }],
      timelineError: "Failed to load timeline: network down",
      timelineLoading: false,
    });
  });

  it("does not overwrite a newer timeline when workspace refresh resolves later", async () => {
    const pendingWorkspace = deferred();

    coreClientMocks.getThreadWorkspace
      .mockResolvedValueOnce({
        thread: { id: "thread-1", title: "Initial workspace" },
        context: {
          recent_events: [{ id: "event-seed", type: "message_posted" }],
          documents: [],
          open_cards: [],
        },
      })
      .mockReturnValueOnce(pendingWorkspace.promise);

    coreClientMocks.listThreadTimeline
      .mockResolvedValueOnce({
        events: [{ id: "event-old", type: "message_posted" }],
      })
      .mockResolvedValueOnce({
        events: [{ id: "event-new", type: "message_posted" }],
      });

    await topicDetailStore.loadWorkspace("thread-1");
    await topicDetailStore.loadTimeline("thread-1");

    const workspaceRefresh = topicDetailStore.loadWorkspace("thread-1");
    await topicDetailStore.loadTimeline("thread-1");

    pendingWorkspace.resolve({
      thread: { id: "thread-1", title: "Refreshed workspace" },
      context: {
        recent_events: [{ id: "event-stale", type: "message_posted" }],
        documents: [{ id: "doc-1", title: "Doc 1" }],
        open_cards: [],
      },
    });

    await workspaceRefresh;

    expect(get(topicDetailStore)).toMatchObject({
      topic: { id: "thread-1", title: "Refreshed workspace" },
      timelineThreadId: "thread-1",
      timeline: [{ id: "event-new", type: "message_posted" }],
      documents: [{ id: "doc-1", title: "Doc 1" }],
    });
  });

  it("ignores stale timeline failures after a newer request succeeds", async () => {
    const firstRequest = deferred();

    coreClientMocks.listThreadTimeline
      .mockReturnValueOnce(firstRequest.promise)
      .mockResolvedValueOnce({
        events: [{ id: "event-new", type: "message_posted" }],
      });

    const firstLoad = topicDetailStore.loadTimeline("thread-1");
    const secondLoad = topicDetailStore.loadTimeline("thread-1");

    await secondLoad;

    firstRequest.reject(new Error("old request failed"));
    await expect(firstLoad).resolves.toBeUndefined();

    expect(get(topicDetailStore)).toMatchObject({
      timelineThreadId: "thread-1",
      timeline: [{ id: "event-new", type: "message_posted" }],
      timelineError: "",
      timelineLoading: false,
    });
  });

  it("does not reuse timeline state across different threads", async () => {
    coreClientMocks.getThreadWorkspace
      .mockResolvedValueOnce({
        thread: { id: "thread-1", title: "Thread 1" },
        context: {
          recent_events: [{ id: "event-a", type: "message_posted" }],
          documents: [],
          open_cards: [],
        },
      })
      .mockResolvedValueOnce({
        thread: { id: "thread-2", title: "Thread 2" },
        context: {
          recent_events: [{ id: "event-b", type: "message_posted" }],
          documents: [],
          open_cards: [],
        },
      });

    coreClientMocks.listThreadTimeline.mockResolvedValueOnce({
      events: [{ id: "event-a-full", type: "message_posted" }],
    });

    await topicDetailStore.loadWorkspace("thread-1");
    await topicDetailStore.loadTimeline("thread-1");
    await topicDetailStore.loadWorkspace("thread-2");

    expect(get(topicDetailStore)).toMatchObject({
      topic: { id: "thread-2", title: "Thread 2" },
      timelineThreadId: "thread-2",
      timeline: [{ id: "event-b", type: "message_posted" }],
    });
  });

  it("clears timeline on failure when the cached events belong to another thread", async () => {
    coreClientMocks.listThreadTimeline
      .mockResolvedValueOnce({
        events: [{ id: "event-a", type: "message_posted" }],
      })
      .mockRejectedValueOnce(new Error("network down"));

    await topicDetailStore.loadTimeline("thread-1");
    await topicDetailStore.loadTimeline("thread-2");

    expect(get(topicDetailStore)).toMatchObject({
      timelineThreadId: "",
      timeline: [],
      timelineError: "Failed to load timeline: network down",
      timelineLoading: false,
    });
  });

  it("topic detail uses topic workspace, timeline, and backing thread for packets", async () => {
    const topicId = "topic-99";
    const threadRow = {
      id: "thread-backing-99",
      title: "Example thread",
      topic_ref: `topic:${topicId}`,
      updated_at: "2026-01-01T00:00:00Z",
      updated_by: "actor-1",
    };
    coreClientMocks.getTopicWorkspace.mockResolvedValueOnce({
      topic: {
        id: topicId,
        thread_id: threadRow.id,
        title: "Topic title",
        summary: "S",
        type: "other",
        status: "active",
        owner_refs: [],
        document_refs: [],
        board_refs: [],
        related_refs: [],
        created_at: "2026-01-01T00:00:00Z",
        created_by: "actor-1",
        updated_at: "2026-01-01T00:00:00Z",
        updated_by: "actor-1",
        provenance: { sources: [] },
      },
      documents: [{ id: "doc-1", title: "D1" }],
      boards: [],
      cards: [],
      threads: [threadRow],
      inbox: [],
      projection_freshness: {},
      generated_at: "2026-01-01T00:00:00Z",
    });
    coreClientMocks.listTopicTimeline.mockResolvedValueOnce({
      events: [{ id: "evt-t1", type: "message_posted" }],
    });

    await topicDetailStore.fullRefresh(topicId, { asTopic: true });
    await topicDetailStore.loadTimeline(topicId);

    expect(coreClientMocks.getTopicWorkspace).toHaveBeenCalledWith(topicId, {});
    expect(coreClientMocks.getThreadWorkspace).not.toHaveBeenCalled();
    expect(coreClientMocks.listTopicTimeline).toHaveBeenCalledWith(topicId);
    expect(coreClientMocks.listThreadTimeline).not.toHaveBeenCalled();
    expect(get(topicDetailStore)).toMatchObject({
      detailAsTopic: true,
      topic: threadRow,
      documents: [{ id: "doc-1", title: "D1" }],
      timelineThreadId: topicId,
      timeline: [{ id: "evt-t1", type: "message_posted" }],
    });
  });

  it("treats boards linked via refs.topic as owned in topic workspace", async () => {
    const topicId = "topic-refs-own";
    coreClientMocks.getTopicWorkspace.mockResolvedValueOnce({
      topic: {
        id: topicId,
        thread_id: "th-refs",
        title: "Topic",
        updated_at: "2026-01-01T00:00:00Z",
      },
      boards: [
        {
          id: "board-refs-only",
          title: "Board via refs",
          status: "active",
          refs: [`topic:${topicId}`],
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
      cards: [],
      threads: [{ id: "th-refs", topic_ref: `topic:${topicId}` }],
      documents: [],
      inbox: [],
      projection_freshness: {},
      generated_at: "2026-01-01T00:00:00Z",
    });

    await topicDetailStore.fullRefresh(topicId, { asTopic: true });

    expect(get(topicDetailStore).ownedBoards).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          id: "board-refs-only",
          title: "Board via refs",
        }),
      ]),
    );
  });

  it("asTopic: parallel workspace+timeline refresh keeps same-topic scope (empty timeline ok)", async () => {
    const topicId = "topic-post-refresh";
    const workspacePayload = {
      topic: {
        id: topicId,
        thread_id: "th-1",
        title: "Topic",
        summary: "",
        type: "other",
        status: "active",
        owner_refs: [],
        document_refs: [],
        board_refs: [],
        related_refs: [],
        created_at: "2026-01-01T00:00:00Z",
        created_by: "actor-1",
        updated_at: "2026-01-01T00:00:00Z",
        updated_by: "actor-1",
        provenance: { sources: [] },
      },
      documents: [],
      boards: [],
      cards: [],
      threads: [{ id: "th-1", topic_ref: `topic:${topicId}`, title: "T" }],
      inbox: [],
      projection_freshness: {},
      generated_at: "2026-01-01T00:00:00Z",
    };
    coreClientMocks.getTopicWorkspace
      .mockResolvedValueOnce(workspacePayload)
      .mockResolvedValueOnce(workspacePayload);
    coreClientMocks.listTopicTimeline.mockResolvedValueOnce({
      events: [{ id: "m1", type: "message_posted", thread_id: "th-1" }],
    });

    await topicDetailStore.fullRefresh(topicId, { asTopic: true });
    expect(get(topicDetailStore).timeline).toEqual([]);

    await topicDetailStore.refreshTopicDetail(topicId, {
      workspace: true,
      timeline: true,
    });

    expect(get(topicDetailStore)).toMatchObject({
      timelineThreadId: topicId,
      timeline: [{ id: "m1", type: "message_posted", thread_id: "th-1" }],
    });
  });

  it("asTopic: workspace load clears timeline only when route topic id changes", async () => {
    const workspacePayload = (topicId) => ({
      topic: {
        id: topicId,
        thread_id: `th-${topicId}`,
        title: `Topic ${topicId}`,
        summary: "",
        type: "other",
        status: "active",
        owner_refs: [],
        document_refs: [],
        board_refs: [],
        related_refs: [],
        created_at: "2026-01-01T00:00:00Z",
        created_by: "actor-1",
        updated_at: "2026-01-01T00:00:00Z",
        updated_by: "actor-1",
        provenance: { sources: [] },
      },
      documents: [],
      boards: [],
      cards: [],
      threads: [{ id: `th-${topicId}`, topic_ref: `topic:${topicId}` }],
      inbox: [],
      projection_freshness: {},
      generated_at: "2026-01-01T00:00:00Z",
    });

    coreClientMocks.getTopicWorkspace
      .mockResolvedValueOnce(workspacePayload("topic-a"))
      .mockResolvedValueOnce(workspacePayload("topic-b"));

    await topicDetailStore.fullRefresh("topic-a", { asTopic: true });
    coreClientMocks.listTopicTimeline.mockResolvedValueOnce({
      events: [{ id: "e-a", type: "message_posted" }],
    });
    await topicDetailStore.loadTimeline("topic-a", { asTopic: true });

    await topicDetailStore.loadWorkspace("topic-b", { asTopic: true });

    expect(get(topicDetailStore)).toMatchObject({
      topic: expect.objectContaining({ id: `th-topic-b` }),
      timeline: [],
      timelineThreadId: "",
    });
  });

  it("coalesces queued refresh requests while a refresh is in flight", async () => {
    const firstRefresh = deferred();

    coreClientMocks.getThreadWorkspace
      .mockResolvedValueOnce({
        thread: { id: "thread-1", title: "Initial workspace" },
        context: {
          recent_events: [],
          documents: [],
          open_cards: [],
        },
      })
      .mockReturnValueOnce(firstRefresh.promise)
      .mockResolvedValueOnce({
        thread: { id: "thread-1", title: "Updated workspace" },
        context: {
          recent_events: [{ id: "event-2", type: "message_posted" }],
          documents: [{ id: "doc-2", title: "Concurrent doc" }],
          open_cards: [{ id: "card-2", title: "Blocked" }],
        },
      });
    coreClientMocks.listThreadTimeline.mockResolvedValue({
      events: [{ id: "event-2", type: "message_posted" }],
    });

    await topicDetailStore.loadWorkspace("thread-1");

    const firstPromise = topicDetailStore.queueRefreshTopicDetail("thread-1", {
      workspace: true,
    });
    const secondPromise = topicDetailStore.queueRefreshTopicDetail("thread-1", {
      timeline: true,
    });

    firstRefresh.resolve({
      thread: { id: "thread-1", title: "First refresh" },
      context: {
        recent_events: [],
        documents: [],
        open_cards: [],
      },
    });

    await Promise.all([firstPromise, secondPromise]);

    expect(coreClientMocks.getThreadWorkspace).toHaveBeenCalledTimes(2);
    expect(coreClientMocks.listThreadTimeline).toHaveBeenCalledTimes(1);
    expect(get(topicDetailStore)).toMatchObject({
      topic: { id: "thread-1", title: "First refresh" },
      timeline: [{ id: "event-2", type: "message_posted" }],
      documents: [],
    });
  });
});
