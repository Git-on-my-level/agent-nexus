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
    humanIds: new Set(["operator"]),
    work: [
      {
        ref: "card:human",
        phase: "ready",
        next_actor: "actor:operator",
        next_action: "Approve pilot",
      },
      { ref: "card:agent", phase: "ready", next_actor: "actor:claude" },
      { ref: "card:done", phase: "done", next_actor: "actor:operator" },
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

it("preserves the backend plan geometry and visit digest wire fixtures", async () => {
  const { loadOverview } = await import("../../src/lib/overview.js");
  const { default: tile } =
    await import("../../../contracts/fixtures/initiative-overview/tile.json");
  const { default: digest } =
    await import("../../../contracts/fixtures/initiative-overview/digest.json");
  const initiatives = {
    status: "ok",
    count: 1,
    items: [tile],
    truncated: true,
  };
  const result = await loadOverview({
    getOverview: async () => ({
      work: {
        status: "ok",
        items: [],
        total: 0,
        human_count: 0,
        truncated: true,
      },
      initiatives,
      since_you_last_looked: digest,
      needs_you: { status: "ok", rows: [], count: 0 },
      dashboard: { status: "ok", reports: [] },
      agents: { status: "unavailable" },
    }),
  });
  expect(result.initiatives).toBe(initiatives);
  expect(result.sinceYouLastLooked).toBe(digest);
  expect(result.work.truncated).toBe(true);
});

it("preserves partial agent counts and accumulated dashboard choices", async () => {
  const { loadOverview, mergeDashboardReports } =
    await import("../../src/lib/overview.js");
  const { visualReportExample } =
    await import("../../src/lib/fixtures/visualReportExample.js");
  const model = await loadOverview({
    getOverview: async () => ({
      work: { status: "ok", items: [], human_count: 0 },
      needs_you: { status: "ok", rows: [] },
      dashboard: { status: "ok", reports: [] },
      agents: { status: "ok", truncated: true, items: [{ state: "idle" }] },
    }),
  });
  expect(model.agents).toMatchObject({ working: 0, truncated: true });
  const first = {
    status: "ok",
    pinned_ref: "document:first",
    reports: [{ id: "first", report: visualReportExample }],
    next_cursor: "two",
    has_more: true,
  };
  const merged = mergeDashboardReports(first, {
    status: "ok",
    pinned_ref: null,
    reports: [
      { id: "first", report: visualReportExample },
      { id: "second", report: visualReportExample },
    ],
    has_more: false,
  });
  expect(merged.reports.map((r) => r.id)).toEqual(["first", "second"]);
  expect(merged.pinned_ref).toBe("document:first");
  expect(merged.has_more).toBe(false);
});
