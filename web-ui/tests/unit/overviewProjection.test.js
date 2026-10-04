import { expect, it } from "vitest";
import { loadInboxSources } from "../../src/lib/inboxSources.js";
import { buildInboxRows, filterMailbox } from "../../src/lib/inboxMailbox.js";

it("excludes archived work decisions from Watching using the core lifecycle projection", async () => {
  const results = await loadInboxSources({
    withHistory: false,
    client: {
      listPmDecisions: async () => ({
        items: [
          { id: "old", work_ref: "card:old", status: "answered" },
          { id: "live", work_ref: "card:live", status: "answered" },
        ],
      }),
      listPmActions: async () => ({
        items: [{ id: "old-action", work_ref: "card:old" }],
      }),
      listWork: async () => ({ work: [], archived_refs: ["card:old"] }),
      listInboxItems: async () => ({ items: [] }),
    },
  });
  expect(results[0].value.items.map((item) => item.id)).toEqual(["live"]);
  expect(results[1].value.items).toEqual([]);
});

it("keeps human next actors in Needs you and prioritizes asks over routine Watching edits", () => {
  const rows = buildInboxRows({
    humanIds: new Set(["david"]),
    work: [
      {
        ref: "card:human",
        phase: "ready",
        next_actor: "actor:david",
        next_action: "Approve pilot",
      },
      { ref: "card:agent", phase: "ready", next_actor: "actor:claude" },
      { ref: "card:done", phase: "done", next_actor: "actor:david" },
    ],
    updates: [
      {
        group_ref: "board:edits",
        newest_event: { ts: "2026-10-04T12:00:00Z" },
        events: [{ type: "card_updated" }],
      },
      {
        group_ref: "board:ask",
        newest_event: { ts: "2026-10-01T12:00:00Z" },
        events: [{ type: "human_attention_requested" }],
      },
    ],
  });
  expect(filterMailbox(rows, "needs-you").map((row) => row.ref)).toEqual([
    "card:human",
  ]);
  expect(filterMailbox(rows, "watching").map((row) => row.ref)).toEqual([
    "board:ask",
    "board:edits",
  ]);
});
