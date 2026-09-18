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
        recent_events: [{ id: "event-seed", type: "message_posted" }],
        documents: [],
        open_cards: [],
      },
    });

    await topicDetailStore.loadWorkspace("thread-1");

    expect(get(topicDetailStore).timeline).toEqual([
      { id: "event-seed", type: "message_posted" },
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
        recent_events: [{ id: "event-1", type: "message_posted" }],
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

  it("patches streamed receipts by trigger event and wakeup id", () => {
    topicDetailStore.reset();
    topicDetailStore.setTimeline([], "thread-1");

    topicDetailStore.patchNotificationReceipt({
      wakeup_id: "wake-1",
      trigger_event_id: "event-1",
      delivery_status: "requested",
    });
    topicDetailStore.patchNotificationReceipt({
      wakeup_id: "wake-2",
      trigger_event_id: "event-1",
      delivery_status: "requested",
    });
    topicDetailStore.patchNotificationReceipt({
      wakeup_id: "wake-1",
      trigger_event_id: "event-1",
      delivery_status: "completed",
    });

    const receipts = get(topicDetailStore).timelineNotificationReceipts;
    expect(receipts["event-1"]).toEqual([
      {
        wakeup_id: "wake-1",
        trigger_event_id: "event-1",
        delivery_status: "completed",
      },
      {
        wakeup_id: "wake-2",
        trigger_event_id: "event-1",
        delivery_status: "requested",
      },
    ]);
  });

  it("does not let a stale timeline response downgrade a streamed receipt", async () => {
    topicDetailStore.reset();
    topicDetailStore.setTimeline([], "thread-1");
    topicDetailStore.patchNotificationReceipt({
      wakeup_id: "wake-1",
      trigger_event_id: "event-1",
      delivery_status: "completed",
      completed_at: "2026-05-04T08:02:00Z",
    });
    coreClientMocks.listThreadTimeline.mockResolvedValueOnce({
      events: [{ id: "event-1", type: "message_posted" }],
      notification_receipts: {
        "event-1": [
          {
            wakeup_id: "wake-1",
            trigger_event_id: "event-1",
            delivery_status: "requested",
            created_at: "2026-05-04T08:00:00Z",
          },
        ],
      },
    });

    await topicDetailStore.loadTimeline("thread-1");

    expect(
      get(topicDetailStore).timelineNotificationReceipts["event-1"][0],
    ).toMatchObject({
      wakeup_id: "wake-1",
      delivery_status: "completed",
    });
  });

  it("does not let a stale streamed receipt downgrade fresher timeline state", () => {
    topicDetailStore.reset();
    topicDetailStore.setTimeline([], "thread-1");
    topicDetailStore.patchNotificationReceipt({
      wakeup_id: "wake-1",
      trigger_event_id: "event-1",
      delivery_status: "completed",
      completed_at: "2026-05-04T08:02:00Z",
    });
    topicDetailStore.patchNotificationReceipt({
      wakeup_id: "wake-1",
      trigger_event_id: "event-1",
      delivery_status: "requested",
      created_at: "2026-05-04T08:00:00Z",
    });

    expect(
      get(topicDetailStore).timelineNotificationReceipts["event-1"][0],
    ).toMatchObject({
      wakeup_id: "wake-1",
      delivery_status: "completed",
    });
  });
});
