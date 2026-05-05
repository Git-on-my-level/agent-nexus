// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const pageStore = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/local/w/local/inbox"),
    params: {
      organization: "local",
      workspace: "local",
    },
  };
  const subscribers = new Set();
  return {
    subscribe(fn) {
      subscribers.add(fn);
      fn(value);
      return () => subscribers.delete(fn);
    },
    set(next) {
      value = next;
      for (const fn of subscribers) fn(value);
    },
    reset() {
      this.set({
        url: new URL("http://localhost/o/local/w/local/inbox"),
        params: {
          organization: "local",
          workspace: "local",
        },
      });
    },
  };
});

const navigationMock = vi.hoisted(() => {
  const afterNavigateCallbacks = new Set();
  return {
    goto: vi.fn(),
    invalidate: vi.fn(),
    invalidateAll: vi.fn(),
    beforeNavigate: vi.fn(),
    afterNavigate: vi.fn((fn) => {
      afterNavigateCallbacks.add(fn);
    }),
    reset() {
      afterNavigateCallbacks.clear();
    },
    triggerAfterNavigate() {
      for (const fn of afterNavigateCallbacks) fn();
    },
  };
});

const coreClientMock = vi.hoisted(() => ({
  archiveArtifact: vi.fn(),
  archiveTopic: vi.fn(),
  createTopic: vi.fn(),
  listArtifacts: vi.fn(),
  listInboxItems: vi.fn(),
  listThreads: vi.fn(),
  listTopics: vi.fn(),
  respondInboxItem: vi.fn(),
  trashArtifact: vi.fn(),
  trashTopic: vi.fn(),
  unarchiveArtifact: vi.fn(),
  unarchiveTopic: vi.fn(),
}));

vi.mock("$app/navigation", () => ({
  goto: navigationMock.goto,
  invalidate: navigationMock.invalidate,
  invalidateAll: navigationMock.invalidateAll,
  beforeNavigate: navigationMock.beforeNavigate,
  afterNavigate: navigationMock.afterNavigate,
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: pageStore.subscribe,
  },
}));

vi.mock("$lib/coreClient", () => ({
  coreClient: coreClientMock,
}));

import ArtifactListPage from "../../src/routes/o/[organization]/w/[workspace]/artifacts/+page.svelte";
import InboxPage from "../../src/routes/o/[organization]/w/[workspace]/inbox/+page.svelte";

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function setRoute(path) {
  pageStore.set({
    url: new URL(`http://localhost${path}`),
    params: {
      organization: "local",
      workspace: "local",
    },
  });
}

function navigateTo(path) {
  setRoute(path);
  navigationMock.triggerAfterNavigate();
}

function inboxItem(id, title) {
  return {
    id,
    title,
    kind: "ask",
    category: "ask",
    requester_actor_id: "actor-test",
    related_refs: ["thread:thread-test"],
    source_event_time: "2026-05-05T00:00:00Z",
    subject_ref: "thread:thread-test",
  };
}

function completedInboxItem(id, title) {
  return {
    id,
    title,
    kind: "ask",
    response_text: "Done.",
    responded_at: "2026-05-05T00:00:00Z",
    responding_actor_id: "actor-test",
  };
}

function artifact(id, summary) {
  return {
    id,
    summary,
    kind: "doc",
    refs: [],
    state: "active",
    created_at: "2026-05-05T00:00:00Z",
    created_by: "actor-test",
  };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  navigationMock.reset();
  pageStore.reset();
});

beforeEach(() => {
  pageStore.reset();
});

describe("remaining web-ui list stale loads", () => {
  it("keeps newer open inbox rows when an older route load resolves late", async () => {
    const slowOldInbox = deferred();
    coreClientMock.listInboxItems
      .mockReturnValueOnce(slowOldInbox.promise)
      .mockResolvedValueOnce({
        items: [inboxItem("inbox-new", "New inbox row")],
      });

    setRoute("/o/local/w/local/inbox");
    render(InboxPage);

    await waitFor(() => {
      expect(coreClientMock.listInboxItems).toHaveBeenCalledTimes(1);
    });

    navigateTo("/o/local/w/local/inbox?category=ask");

    await waitFor(() => {
      expect(coreClientMock.listInboxItems).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.getByText("New inbox row")).toBeTruthy();
    });

    slowOldInbox.resolve({
      items: [inboxItem("inbox-old", "Old inbox row")],
    });
    await Promise.resolve();

    expect(screen.getByText("New inbox row")).toBeTruthy();
    expect(screen.queryByText("Old inbox row")).not.toBeTruthy();
  });

  it("keeps newer completed inbox rows when an older filter load resolves late", async () => {
    const slowOldCompleted = deferred();
    coreClientMock.listInboxItems
      .mockReturnValueOnce(slowOldCompleted.promise)
      .mockResolvedValueOnce({
        items: [completedInboxItem("completed-new", "New completed row")],
      });

    setRoute("/o/local/w/local/inbox?status=completed&window_days=7");
    render(InboxPage);

    await waitFor(() => {
      expect(coreClientMock.listInboxItems).toHaveBeenCalledTimes(1);
    });

    navigateTo(
      "/o/local/w/local/inbox?status=completed&completed_kind=review&window_days=30",
    );

    await waitFor(() => {
      expect(coreClientMock.listInboxItems).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.getByText("New completed row")).toBeTruthy();
    });

    slowOldCompleted.resolve({
      items: [completedInboxItem("completed-old", "Old completed row")],
    });
    await Promise.resolve();

    expect(screen.getByText("New completed row")).toBeTruthy();
    expect(screen.queryByText("Old completed row")).not.toBeTruthy();
  });

  it("keeps newer artifact rows when an older URL filter response resolves late", async () => {
    const slowOldArtifacts = deferred();
    coreClientMock.listArtifacts
      .mockReturnValueOnce(slowOldArtifacts.promise)
      .mockResolvedValueOnce({
        artifacts: [artifact("artifact-new", "New artifact row")],
      });

    setRoute("/o/local/w/local/artifacts?kind=doc");
    render(ArtifactListPage);

    await waitFor(() => {
      expect(coreClientMock.listArtifacts).toHaveBeenCalledTimes(1);
    });

    setRoute("/o/local/w/local/artifacts?kind=card");

    await waitFor(() => {
      expect(coreClientMock.listArtifacts).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.getByText("New artifact row")).toBeTruthy();
    });

    slowOldArtifacts.resolve({
      artifacts: [artifact("artifact-old", "Old artifact row")],
    });
    await Promise.resolve();

    expect(screen.getByText("New artifact row")).toBeTruthy();
    expect(screen.queryByText("Old artifact row")).not.toBeTruthy();
  });
});
