// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const pageStore = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/local/threads?q=old"),
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
        url: new URL("http://localhost/o/local/threads?q=old"),
        params: {
          organization: "local",
          workspace: "local",
        },
      });
    },
  };
});

const coreClientMock = vi.hoisted(() => ({
  listThreads: vi.fn(),
}));

vi.mock("$app/navigation", () => ({
  goto: vi.fn(),
  invalidate: vi.fn(),
  invalidateAll: vi.fn(),
  beforeNavigate: vi.fn(),
  afterNavigate: vi.fn(),
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: pageStore.subscribe,
  },
}));

vi.mock("$lib/coreClient", () => ({
  coreClient: coreClientMock,
}));

import ThreadListPage from "../../src/routes/o/[organization]/w/[workspace]/threads/+page.svelte";

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function thread(id, title) {
  return {
    id,
    title,
    state: "active",
    topic_ref: "",
    updated_at: "2026-05-05T00:00:00Z",
  };
}

function setThreadsSearch(q) {
  pageStore.set({
    url: new URL(`http://localhost/o/local/w/local/threads?q=${q}`),
    params: {
      organization: "local",
      workspace: "local",
    },
  });
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  pageStore.reset();
});

beforeEach(() => {
  pageStore.reset();
});

describe("threads list stale loads", () => {
  it("keeps newer thread rows when an older filter response resolves late", async () => {
    const slowOldThreads = deferred();
    coreClientMock.listThreads
      .mockReturnValueOnce(slowOldThreads.promise)
      .mockResolvedValueOnce({
        threads: [thread("thread-new", "New thread row")],
      });

    setThreadsSearch("old");
    render(ThreadListPage);

    await waitFor(() => {
      expect(coreClientMock.listThreads).toHaveBeenCalledTimes(1);
    });

    setThreadsSearch("new");

    await waitFor(() => {
      expect(coreClientMock.listThreads).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.getByText("New thread row")).toBeTruthy();
    });

    slowOldThreads.resolve({
      threads: [thread("thread-old", "Old thread row")],
    });
    await Promise.resolve();

    expect(screen.getByText("New thread row")).toBeTruthy();
    expect(screen.queryByText("Old thread row")).not.toBeTruthy();
  });
});
