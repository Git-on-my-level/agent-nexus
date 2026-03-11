import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const coreClientMocks = vi.hoisted(() => ({
  getThreadWorkspace: vi.fn(),
  listThreadTimeline: vi.fn(),
  listArtifacts: vi.fn(),
}));

vi.mock("../../src/lib/coreClient.js", () => ({
  coreClient: {
    getThreadWorkspace: coreClientMocks.getThreadWorkspace,
    listThreadTimeline: coreClientMocks.listThreadTimeline,
    listArtifacts: coreClientMocks.listArtifacts,
  },
}));

import { threadDetailStore } from "../../src/lib/threadDetailStore.js";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("threadDetailStore", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    threadDetailStore.reset();
  });

  afterEach(() => {
    threadDetailStore.reset();
  });

  it("keeps existing workspace content mounted during background refresh", async () => {
    coreClientMocks.getThreadWorkspace.mockResolvedValueOnce({
      thread: { id: "thread-1", title: "Initial workspace" },
      context: {
        recent_events: [],
        documents: [],
        open_commitments: [],
      },
    });

    await threadDetailStore.loadWorkspace("thread-1");

    const pendingRefresh = deferred();
    coreClientMocks.getThreadWorkspace.mockReturnValueOnce(
      pendingRefresh.promise,
    );

    const refreshPromise = threadDetailStore.refreshThreadDetail("thread-1", {
      workspace: true,
    });

    expect(get(threadDetailStore).snapshotLoading).toBe(false);
    expect(get(threadDetailStore).snapshot).toMatchObject({
      id: "thread-1",
      title: "Initial workspace",
    });

    pendingRefresh.resolve({
      thread: { id: "thread-1", title: "Refreshed workspace" },
      context: {
        recent_events: [{ id: "event-1", type: "actor_statement" }],
        documents: [{ id: "doc-1", title: "Doc 1" }],
        open_commitments: [{ id: "commit-1", status: "open" }],
      },
    });

    await refreshPromise;

    expect(get(threadDetailStore)).toMatchObject({
      snapshot: { id: "thread-1", title: "Refreshed workspace" },
      timeline: [{ id: "event-1", type: "actor_statement" }],
      documents: [{ id: "doc-1", title: "Doc 1" }],
      commitments: [{ id: "commit-1", status: "open" }],
      snapshotLoading: false,
      snapshotError: "",
      documentsError: "",
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
          open_commitments: [],
        },
      })
      .mockReturnValueOnce(firstRefresh.promise)
      .mockResolvedValueOnce({
        thread: { id: "thread-1", title: "Updated workspace" },
        context: {
          recent_events: [{ id: "event-2", type: "message_posted" }],
          documents: [{ id: "doc-2", title: "Concurrent doc" }],
          open_commitments: [{ id: "commit-2", status: "blocked" }],
        },
      });
    coreClientMocks.listThreadTimeline.mockResolvedValue({
      events: [{ id: "event-2", type: "message_posted" }],
    });
    coreClientMocks.listArtifacts.mockResolvedValue({
      artifacts: [{ id: "artifact-2", kind: "work_order" }],
    });

    await threadDetailStore.loadWorkspace("thread-1");

    const firstPromise = threadDetailStore.queueRefreshThreadDetail(
      "thread-1",
      {
        workspace: true,
      },
    );
    const secondPromise = threadDetailStore.queueRefreshThreadDetail(
      "thread-1",
      {
        timeline: true,
        workOrders: true,
      },
    );

    firstRefresh.resolve({
      thread: { id: "thread-1", title: "First refresh" },
      context: {
        recent_events: [],
        documents: [],
        open_commitments: [],
      },
    });

    await Promise.all([firstPromise, secondPromise]);

    expect(coreClientMocks.getThreadWorkspace).toHaveBeenCalledTimes(2);
    expect(coreClientMocks.listThreadTimeline).toHaveBeenCalledTimes(1);
    expect(coreClientMocks.listArtifacts).toHaveBeenCalledTimes(1);
    expect(get(threadDetailStore)).toMatchObject({
      snapshot: { id: "thread-1", title: "First refresh" },
      timeline: [{ id: "event-2", type: "message_posted" }],
      documents: [],
      commitments: [],
      workOrders: [{ id: "artifact-2", kind: "work_order" }],
    });
  });
});
