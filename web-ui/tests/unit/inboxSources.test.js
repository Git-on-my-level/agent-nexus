import { afterEach, describe, expect, it, vi } from "vitest";
import {
  listAllPages,
  loadInboxSources,
  mergeInboxItems,
  mergeInboxSnapshot,
  hasCompleteInboxHistory,
} from "../../src/lib/inboxSources.js";

describe("Inbox sources", () => {
  it("retains missing records across partial reads but replaces a complete snapshot", () => {
    const previous = [{ id: "selected" }, { id: "updated", title: "Old" }];
    const incoming = [{ id: "updated", title: "New" }, { id: "late" }];
    expect(mergeInboxSnapshot(previous, incoming, false)).toEqual([
      previous[0],
      ...incoming,
    ]);
    expect(mergeInboxSnapshot(previous, [], false)).toEqual(previous);
    expect(mergeInboxSnapshot(previous, incoming, true)).toEqual(incoming);
    expect(mergeInboxSnapshot([{ ref: "task" }], [], false, "ref")).toEqual([
      { ref: "task" },
    ]);
  });

  it("retains a known answer when a partial history omits it", () => {
    const answer = { id: "answer", inbox_item_id: "ask", status: "completed" };
    const completed = mergeInboxSnapshot([answer], [{ id: "other" }], false);
    expect(mergeInboxItems([{ id: "ask" }], completed)).toEqual([
      answer,
      { id: "other" },
    ]);
  });

  it("filters archived work from the settled PM snapshot", async () => {
    const progress = [];
    const client = {
      listPmDecisions: async () => ({
        items: [{ id: "decision", work_ref: "card:archived" }],
      }),
      listPmActions: async () => ({
        items: [{ id: "action", work_ref: "card:archived" }],
      }),
      listWork: async () => ({ work: [], archived_refs: ["card:archived"] }),
      listInboxItems: async () => ({ items: [] }),
      getHomeUnread: async () => ({ groups: [] }),
    };
    const results = await loadInboxSources({
      client,
      onProgress: (snapshot) => progress.push(snapshot),
    });
    for (const snapshot of [...progress, results]) {
      if (snapshot[2].status !== "fulfilled") continue;
      for (const index of [0, 1]) {
        if (snapshot[index].status === "fulfilled")
          expect(snapshot[index].value.items).toEqual([]);
      }
    }
  });
  it("removes an older open snapshot using the completed row's original id", () => {
    const completed = {
      id: "completed:response",
      inbox_item_id: "inbox:ask",
      status: "completed",
    };
    expect(
      mergeInboxItems(
        [{ id: "inbox:ask" }, { id: "inbox:other" }],
        [completed],
      ),
    ).toEqual([{ id: "inbox:other" }, completed]);
  });

  afterEach(() => vi.useRealTimers());

  function clientWith(overrides = {}) {
    return {
      listPmDecisions: async () => ({ items: [] }),
      listPmActions: async () => ({ items: [] }),
      listWork: async () => ({ work: [{ ref: "card:answered" }] }),
      listInboxItems: async ({ status }) => ({
        items:
          status === "completed"
            ? [
                {
                  id: "answer",
                  inbox_item_id: "ask",
                  subject_ref: "card:answered",
                },
              ]
            : [{ id: "ask" }],
      }),
      getHomeUnread: async () => ({ groups: [] }),
      ...overrides,
    };
  }

  it("publishes at 800ms when unread never resolves and terminates at the final deadline", async () => {
    vi.useFakeTimers();
    const progress = [];
    const pending = loadInboxSources({
      client: clientWith({ getHomeUnread: () => new Promise(() => {}) }),
      onProgress: (snapshot) => progress.push(snapshot),
    });
    await vi.advanceTimersByTimeAsync(799);
    expect(progress).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(progress).toHaveLength(1);
    expect(progress[0][5].status).toBe("pending");
    expect(hasCompleteInboxHistory(progress[0])).toBe(true);
    expect(
      mergeInboxItems(
        progress[0][3].value.items,
        progress[0][4].value.items,
      ).some((item) => item.id === "ask"),
    ).toBe(false);
    await vi.advanceTimersByTimeAsync(44_200);
    expect((await pending)[5].status).toBe("rejected");
    expect(vi.getTimerCount()).toBe(0);
  });

  it("paints first pages without waiting for slow later work pages", async () => {
    vi.useFakeTimers();
    let finishPage;
    const slowPage = new Promise((resolve) => {
      finishPage = resolve;
    });
    const progress = [];
    const pending = loadInboxSources({
      client: clientWith({
        listWork: ({ cursor, summary, limit }) => {
          expect({ summary, limit }).toEqual({ summary: 1, limit: 50 });
          return cursor
            ? slowPage
            : Promise.resolve({
                work: [{ ref: "card:first" }],
                next_cursor: "next",
              });
        },
      }),
      onProgress: (snapshot) => progress.push(snapshot),
    });
    await vi.advanceTimersByTimeAsync(0);
    expect(progress.length).toBeGreaterThan(0);
    expect(progress[0][2]).toMatchObject({
      complete: false,
      value: { work: [{ ref: "card:first" }], has_more: true },
    });
    finishPage({ work: [{ ref: "card:later" }] });
    const result = await pending;
    expect(result[2].value.work).toEqual([
      { ref: "card:first" },
      { ref: "card:later" },
    ]);
    expect(result[2].complete).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("never classifies work from missing, stalled, capped or partial answer history", async () => {
    vi.useFakeTimers();
    for (const completed of [
      () => new Promise(() => {}),
      async () => ({ items: [], has_more: true }),
      async () => ({ items: [], next_cursor: "repeat" }),
      ({ cursor }) =>
        cursor
          ? new Promise(() => {})
          : Promise.resolve({ items: [], next_cursor: "slow" }),
    ]) {
      const progress = [];
      const pending = loadInboxSources({
        client: clientWith({
          listInboxItems: (options) =>
            options.status === "completed"
              ? completed(options)
              : Promise.resolve({ items: [{ id: "ask" }] }),
        }),
        onProgress: (snapshot) => progress.push(snapshot),
      });
      await vi.advanceTimersByTimeAsync(45_000);
      const result = await pending;
      expect(progress.length).toBeGreaterThan(0);
      for (const snapshot of [...progress, result])
        expect(hasCompleteInboxHistory(snapshot)).toBe(false);
    }
    expect(vi.getTimerCount()).toBe(0);
  });

  it("stops pagination and publication on cancellation, ignoring late responses", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    let finish;
    const slow = new Promise((resolve) => {
      finish = resolve;
    });
    const progress = [];
    const listWork = vi.fn(() => slow);
    const pending = loadInboxSources({
      client: clientWith({ listWork }),
      signal: controller.signal,
      onProgress: (snapshot) => progress.push(snapshot),
    });
    await vi.advanceTimersByTimeAsync(800);
    controller.abort();
    await pending;
    const count = progress.length;
    finish({ work: [], next_cursor: "never-request" });
    await vi.advanceTimersByTimeAsync(45_000);
    expect(progress).toHaveLength(count);
    expect(listWork).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
  });
  it("keeps partial results when a feed repeats its cursor", async () => {
    const calls = [];
    const result = await listAllPages(async (cursor) => {
      calls.push(cursor);
      return {
        items: [{ id: cursor ? "second" : "first" }],
        archived_refs: [cursor ? "card:second" : "card:first"],
        next_cursor: "next",
      };
    }, "items");

    expect(calls).toEqual([undefined, "next"]);
    expect(result).toEqual({
      items: [{ id: "first" }, { id: "second" }],
      archived_refs: ["card:first", "card:second"],
      has_more: true,
    });
  });

  it("stops a longer cursor cycle without discarding any fetched page", async () => {
    const calls = [];
    const next = new Map([
      [undefined, "a"],
      ["a", "b"],
      ["b", "a"],
    ]);
    const result = await listAllPages(async (cursor) => {
      calls.push(cursor);
      return {
        items: [{ id: cursor || "first" }],
        next_cursor: next.get(cursor),
      };
    }, "items");

    expect(calls).toEqual([undefined, "a", "b"]);
    expect(result.items).toEqual([{ id: "first" }, { id: "a" }, { id: "b" }]);
    expect(result.has_more).toBe(true);
  });

  it("still follows distinct cursors up to the page limit", async () => {
    const calls = [];
    const result = await listAllPages(
      async (cursor) => {
        calls.push(cursor);
        return {
          items: [{ id: calls.length }],
          next_cursor: `page-${calls.length + 1}`,
        };
      },
      "items",
      3,
    );

    expect(calls).toEqual([undefined, "page-2", "page-3"]);
    expect(result.items).toEqual([{ id: 1 }, { id: 2 }, { id: 3 }]);
    expect(result.has_more).toBe(true);
  });

  it("loads every open and completed Inbox page for the mailbox projection", async () => {
    const calls = [];
    const client = {
      listPmDecisions: async () => ({ items: [] }),
      listPmActions: async () => ({ items: [] }),
      listWork: async () => ({ work: [] }),
      listInboxItems: async ({ status, cursor }) => {
        calls.push({ status, cursor });
        if (status === "open" && !cursor)
          return { items: [{ id: "open-1" }], next_cursor: "open-next" };
        if (status === "open") return { items: [{ id: "open-2" }] };
        if (!cursor)
          return {
            items: [{ id: "completed-1" }],
            next_cursor: "completed-next",
          };
        return { items: [{ id: "completed-2" }] };
      },
      getHomeUnread: async () => ({ groups: [] }),
    };

    const results = await loadInboxSources({ withHistory: false, client });
    expect(results[3].value.items.map((item) => item.id)).toEqual([
      "open-1",
      "open-2",
    ]);
    expect(results[4].value.items.map((item) => item.id)).toEqual([
      "completed-1",
      "completed-2",
    ]);
    expect(results[3].value.has_more).toBe(false);
    expect(results[4].value.has_more).toBe(false);
    expect(results[5].value).toBeNull();
    expect(calls).toEqual(
      expect.arrayContaining([
        { status: "open", cursor: undefined },
        { status: "open", cursor: "open-next" },
        { status: "completed", cursor: undefined },
        { status: "completed", cursor: "completed-next" },
      ]),
    );
  });
});

it("keeps a feed's partial marker even when it has no usable next cursor", async () => {
  const result = await listAllPages(
    async () => ({ items: [{ id: "first" }], has_more: true }),
    "items",
  );
  expect(result).toMatchObject({ items: [{ id: "first" }], has_more: true });
});

describe("Inbox sources when no PM is running", () => {
  const nonPmFeeds = () => ({
    listWork: async () => ({ work: [{ ref: "card:own" }] }),
    listInboxItems: async () => ({ items: [{ id: "ask" }] }),
    getHomeUnread: async () => ({ groups: [] }),
  });

  /*
   * A PM runs on the reader's own computer, so one can be absent while
   * proposals it filed earlier still wait for a yes — and the Inbox is the
   * only place to answer them. The feeds are read whatever the PM state says.
   */
  it("reads the PM feeds even when no PM is running", async () => {
    const listPmDecisions = vi.fn(async () => ({
      items: [{ id: "decision", work_ref: "card:own" }],
    }));
    const listPmActions = vi.fn(async () => ({ items: [] }));
    const results = await loadInboxSources({
      client: { ...nonPmFeeds(), listPmDecisions, listPmActions },
    });
    expect(listPmDecisions).toHaveBeenCalled();
    expect(results[0].value.items).toEqual([
      { id: "decision", work_ref: "card:own" },
    ]);
    // The reader's own rows still load alongside them.
    expect(results[2].value.work).toEqual([{ ref: "card:own" }]);
    expect(results[3].value.items).toEqual([{ id: "ask" }]);
  });

  /*
   * The reserved per-workspace answer: "nothing is there", not a fault the
   * reader can act on, so the Inbox must not read as broken. A transient PM
   * outage is a different thing and keeps its error (below).
   */
  it("treats a pm_not_onboarded refusal as an empty feed, not a failure", async () => {
    const refusal = () => {
      const error = new Error("no PM is onboarded for this workspace");
      error.status = 409;
      error.body = { error: { code: "pm_not_onboarded" } };
      return Promise.reject(error);
    };
    const results = await loadInboxSources({
      client: {
        ...nonPmFeeds(),
        listPmDecisions: refusal,
        listPmActions: refusal,
      },
    });
    for (const index of [0, 1]) {
      expect(results[index]).toMatchObject({ status: "fulfilled" });
      expect(results[index].value.items).toEqual([]);
    }
  });

  it("still reports any other PM feed failure", async () => {
    const outage = Object.assign(new Error("PM bridge unavailable"), {
      status: 503,
      body: { error: { code: "unavailable" } },
    });
    for (const reason of [new Error("core unreachable"), outage]) {
      const results = await loadInboxSources({
        client: {
          ...nonPmFeeds(),
          listPmDecisions: async () => {
            throw reason;
          },
          listPmActions: async () => ({ items: [] }),
        },
      });
      expect(results[0].status).toBe("rejected");
    }
  });
});
