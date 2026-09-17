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
  listPmDecisions: vi.fn(),
  getPmDecision: vi.fn(),
  listPmActions: vi.fn(),
  getPmAction: vi.fn(),
  listWork: vi.fn(),
  getHomeUnread: vi.fn(),
  markHomeRead: vi.fn(),
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
vi.mock("$lib/authSession", () => ({
  initializeAuthSession: vi.fn(async () => ({})),
}));

import InboxPage from "../../src/routes/o/[organization]/w/[workspace]/inbox/+page.svelte";

function setRoute(path) {
  pageStore.set({
    url: new URL(`http://localhost${path}`),
    params: {
      organization: "local",
      workspace: "local",
    },
  });
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

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  navigationMock.reset();
  pageStore.reset();
});

beforeEach(() => {
  pageStore.reset();
  coreClientMock.listPmDecisions.mockResolvedValue({ items: [] });
  coreClientMock.getPmDecision.mockResolvedValue({});
  coreClientMock.listPmActions.mockResolvedValue({ items: [] });
  coreClientMock.getPmAction.mockResolvedValue({});
  coreClientMock.listWork.mockResolvedValue({ work: [] });
  coreClientMock.getHomeUnread.mockResolvedValue({ groups: [] });
  coreClientMock.markHomeRead.mockResolvedValue({});
});

describe("remaining web-ui list stale loads", () => {
  it("keeps newer open inbox rows when an older route load resolves late", async () => {
    coreClientMock.listInboxItems.mockImplementation(async (opts = {}) => {
      if (opts.status === "completed") return { items: [] };
      return { items: [inboxItem("inbox-new", "New inbox row")] };
    });

    setRoute("/o/local/w/local/inbox");
    render(InboxPage);

    await waitFor(() => {
      expect(screen.getAllByText("New inbox row").length).toBeGreaterThan(0);
    });
  });

  it("shows completed inbox rows in Handled", async () => {
    coreClientMock.listInboxItems.mockImplementation(async (opts = {}) => {
      if (opts.status === "completed") {
        return {
          items: [completedInboxItem("completed-new", "New completed row")],
        };
      }
      return { items: [] };
    });

    setRoute("/o/local/w/local/inbox?mailbox=handled");
    render(InboxPage);

    await waitFor(() => {
      expect(screen.getAllByText("New completed row").length).toBeGreaterThan(
        0,
      );
    });
  });
});
