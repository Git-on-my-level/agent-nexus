import { describe, expect, it } from "vitest";
import { loadInboxSources } from "../../src/lib/inboxSources.js";

describe("Inbox sources", () => {
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
