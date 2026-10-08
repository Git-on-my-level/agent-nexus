import { describe, expect, it } from "vitest";
import {
  listAllPages,
  loadInboxSources,
  mergeInboxItems,
} from "../../src/lib/inboxSources.js";

describe("Inbox sources", () => {
  it("filters archived work from progressive PM snapshots as well as the final lists", async () => {
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

  it("publishes actionable asks before work history finishes and preserves the page bound", async () => {
    let finishWork;
    const work = new Promise((resolve) => {
      finishWork = resolve;
    });
    const progress = [];
    const client = {
      listPmDecisions: async () => ({ items: [] }),
      listPmActions: async () => ({ items: [] }),
      listWork: () => work,
      listInboxItems: async ({ status }) => ({
        items: status === "open" ? [{ id: "inbox:ask" }] : [],
      }),
      getHomeUnread: async () => ({ groups: [] }),
    };
    const pending = loadInboxSources({
      client,
      onProgress: (snapshot) => progress.push(snapshot),
    });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(progress[0][3].value.items).toEqual([{ id: "inbox:ask" }]);
    expect(progress[0][2].status).toBe("pending");
    finishWork({ work: [] });
    expect((await pending)[2].status).toBe("fulfilled");
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
