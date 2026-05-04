// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

const coreClientMock = vi.hoisted(() => ({
  getHomeUnread: vi.fn(),
  markHomeRead: vi.fn(),
}));

vi.mock("$app/environment", () => ({
  browser: true,
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: (fn) => {
      fn({
        url: new URL("http://localhost/o/local/w/local"),
        params: { organization: "local", workspace: "local" },
      });
      return () => {};
    },
  },
}));

vi.mock("$lib/authSession", () => ({
  initializeAuthSession: vi.fn(async () => ({})),
}));

vi.mock("$lib/coreClient", () => ({
  coreClient: coreClientMock,
}));

import HomePage from "../../src/routes/o/[organization]/w/[workspace]/+page.svelte";

function feed(unreadCount, label) {
  return {
    unread_count: unreadCount,
    group_count: 1,
    generated_at: `2026-05-05T00:00:0${unreadCount}Z`,
    groups: [
      {
        group_type: "topic",
        group_ref: "topic:launch",
        display_name: label,
        priority: "P1",
        unread_count: unreadCount,
        newest_event: {
          id: `evt-${unreadCount}`,
          ts: `2026-05-05T00:00:0${unreadCount}Z`,
        },
        events: [
          {
            id: `evt-${unreadCount}`,
            type: "message_posted",
            ts: `2026-05-05T00:00:0${unreadCount}Z`,
            actor: { display_name: "Agent" },
            payload: { body: `${label} body` },
            refs: ["topic:launch"],
          },
        ],
      },
    ],
  };
}

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("Home unread refresh coordination", () => {
  it("keeps a newer manual refresh when an older poll response resolves late", async () => {
    vi.useFakeTimers();
    const slowPoll = deferred();

    coreClientMock.getHomeUnread
      .mockResolvedValueOnce(feed(1, "Initial unread"))
      .mockReturnValueOnce(slowPoll.promise)
      .mockResolvedValueOnce(feed(2, "Newer unread"));

    render(HomePage);

    await waitFor(() => {
      expect(screen.getByText(/1 unread across 1 groups/)).toBeTruthy();
    });

    await vi.advanceTimersByTimeAsync(30_000);

    await waitFor(() => {
      expect(coreClientMock.getHomeUnread).toHaveBeenCalledTimes(2);
    });

    await fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    await waitFor(() => {
      expect(screen.getByText(/2 unread across 1 groups/)).toBeTruthy();
    });

    slowPoll.resolve(feed(9, "Stale poll unread"));
    await Promise.resolve();

    expect(screen.getByText(/2 unread across 1 groups/)).toBeTruthy();
    expect(screen.queryByText(/9 unread across 1 groups/)).not.toBeTruthy();
  });

  it("does not start another interval refresh while the prior poll is pending", async () => {
    vi.useFakeTimers();
    const slowPoll = deferred();

    coreClientMock.getHomeUnread
      .mockResolvedValueOnce(feed(1, "Initial unread"))
      .mockReturnValueOnce(slowPoll.promise);

    render(HomePage);

    await waitFor(() => {
      expect(screen.getByText(/1 unread across 1 groups/)).toBeTruthy();
    });

    await vi.advanceTimersByTimeAsync(30_000);
    await vi.advanceTimersByTimeAsync(30_000);

    expect(coreClientMock.getHomeUnread).toHaveBeenCalledTimes(2);
  });
});
