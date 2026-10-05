import { describe, expect, it } from "vitest";

import { initiativeTiles } from "../../src/lib/initiativeTiles.js";
import {
  asksFromSnapshot,
  criticalInitiatives,
  readWorkspaceOpenAsks,
  urgentBandModel,
} from "../../src/lib/overviewUrgent.js";

const here = { organizationSlug: "scaling", slug: "anx", label: "ANX" };
const there = { organizationSlug: "scaling", slug: "ops", label: "Ops" };

const snapshot = (overrides = {}) => ({
  status: "ok",
  count: 2,
  href: "/inbox",
  rows: [
    { id: "decision:1", title: "Choose the launch date", source: "PM" },
    { id: "ask:2", title: "Approve the adapter contract", source: "Inbox" },
  ],
  ...overrides,
});

const row = (overrides = {}) => ({
  ref: "card:release-b",
  title: "Release B",
  summary: "Ship it.",
  plan_state: null,
  updated_at: "2026-10-01T00:00:00Z",
  ...overrides,
});

describe("asksFromSnapshot", () => {
  it("reads the rows core already scoped to this reader", () => {
    const read = asksFromSnapshot(snapshot(), here);
    expect(read.status).toBe("ok");
    expect(read.count).toBe(2);
    expect(read.rows.map((entry) => entry.title)).toEqual([
      "Choose the launch date",
      "Approve the adapter contract",
    ]);
    // Every row knows which workspace it came from, so the band can link to it.
    expect(read.rows[0].workspace).toBe(here);
  });

  it("falls back to the section href when a row has none", () => {
    expect(asksFromSnapshot(snapshot(), here).rows[0].href).toBe("/inbox");
  });

  it("drops a row with no title rather than rendering a blank link", () => {
    const read = asksFromSnapshot(
      snapshot({ rows: [{ id: "x", title: "  " }] }),
      here,
    );
    expect(read.rows).toEqual([]);
  });

  it("reports an unavailable section rather than claiming zero", () => {
    const read = asksFromSnapshot(
      { status: "unavailable", message: "core refused" },
      here,
    );
    expect(read.status).toBe("unavailable");
    expect(read.message).toBe("core refused");
    expect(read.count).toBe(0);
  });
});

describe("readWorkspaceOpenAsks", () => {
  it("uses the per-workspace open-asks endpoint when core exposes one", async () => {
    const calls = [];
    const client = {
      getOpenAsks: async (filters) => {
        calls.push(filters);
        return {
          items: [{ id: "a1", title: "Sign the release", source: "Inbox" }],
          count: 1,
          has_more: false,
        };
      },
      listInboxItems: async () => {
        throw new Error("should not be reached");
      },
    };
    const read = await readWorkspaceOpenAsks(client, there);
    expect(calls).toHaveLength(1);
    expect(read.status).toBe("ok");
    expect(read.rows[0]).toMatchObject({
      title: "Sign the release",
      workspace: there,
    });
  });

  it("falls back to one page of open inbox items — one request, no paging", async () => {
    let calls = 0;
    const client = {
      listInboxItems: async (filters) => {
        calls += 1;
        expect(filters.status).toBe("open");
        return {
          items: [{ id: "i1", title: "Review the plan", category: "review" }],
          next_cursor: "more",
        };
      },
    };
    const read = await readWorkspaceOpenAsks(client, there);
    expect(calls).toBe(1);
    expect(read.rows[0].title).toBe("Review the plan");
    expect(read.rows[0].source).toBe("review");
    expect(read.truncated).toBe(true);
  });

  it("becomes an unavailable note rather than throwing the band away", async () => {
    const client = {
      listInboxItems: async () => {
        throw new Error("no access to this workspace");
      },
    };
    const read = await readWorkspaceOpenAsks(client, there);
    expect(read.status).toBe("unavailable");
    expect(read.message).toContain("no access");
    expect(read.workspace).toBe(there);
  });
});

describe("criticalInitiatives", () => {
  it("keeps only initiatives that have stopped moving, worst first", () => {
    const tiles = initiativeTiles([
      row({ ref: "card:ok", health: { status: "on_track" } }),
      row({ ref: "card:stale", health: { status: "stalled" } }),
      row({ ref: "card:blocked", health: { status: "blocked" } }),
      row({ ref: "card:risk", plan_health: { state: "at_risk" } }),
      row({ ref: "card:none" }),
    ]);
    expect(criticalInitiatives(tiles).map((tile) => tile.ref)).toEqual([
      "card:blocked",
      "card:risk",
      "card:stale",
    ]);
  });

  it("tolerates a missing list", () => {
    expect(criticalInitiatives()).toEqual([]);
    expect(criticalInitiatives(null)).toEqual([]);
  });
});

describe("urgentBandModel", () => {
  const tiles = initiativeTiles([
    row({ ref: "card:blocked", health: { status: "blocked" } }),
    row({ ref: "card:ok", health: { status: "on_track" } }),
  ]);

  it("merges asks from every workspace that answered", () => {
    const band = urgentBandModel({
      asks: [
        asksFromSnapshot(snapshot(), here),
        {
          status: "ok",
          workspace: there,
          rows: [{ id: "o1", title: "Sign off", workspace: there }],
          count: 1,
        },
      ],
      tiles,
    });
    expect(band.asks.count).toBe(3);
    expect(band.asks.rows).toHaveLength(3);
    expect(band.asks.workspaces).toBe(2);
    expect(band.empty).toBe(false);
  });

  it("puts critical initiatives in the band and leaves healthy ones out", () => {
    const band = urgentBandModel({ asks: [], tiles });
    expect(band.initiatives.rows.map((tile) => tile.ref)).toEqual([
      "card:blocked",
    ]);
    expect(band.initiatives.count).toBe(1);
  });

  it("names the workspaces it could not read instead of going quiet", () => {
    const band = urgentBandModel({
      asks: [
        asksFromSnapshot(snapshot(), here),
        { status: "unavailable", workspace: there, message: "no access" },
      ],
      tiles: [],
    });
    expect(band.unavailable).toEqual([
      { workspace: there, message: "no access" },
    ]);
    expect(band.asks.count).toBe(2);
  });

  it("is empty when nothing is waiting and nothing has stalled", () => {
    const band = urgentBandModel({
      asks: [asksFromSnapshot(snapshot({ count: 0, rows: [] }), here)],
      tiles: initiativeTiles([
        row({ ref: "card:ok", health: { status: "on_track" } }),
      ]),
    });
    expect(band.empty).toBe(true);
    expect(band.asks.rows).toEqual([]);
    expect(band.initiatives.rows).toEqual([]);
  });

  it("caps the rows it shows and says there are more", () => {
    const many = Array.from({ length: 10 }, (_, index) => ({
      id: `a${index}`,
      title: `Ask ${index}`,
      workspace: here,
    }));
    const band = urgentBandModel({
      asks: [{ status: "ok", workspace: here, rows: many, count: 10 }],
      tiles: [],
      askLimit: 3,
    });
    expect(band.asks.rows).toHaveLength(3);
    expect(band.asks.truncated).toBe(true);
  });

  it("tolerates being called with nothing", () => {
    const band = urgentBandModel();
    expect(band.empty).toBe(true);
    expect(band.unavailable).toEqual([]);
  });
});
