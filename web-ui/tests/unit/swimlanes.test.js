import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../src/lib/mutationOutbox.js", () => ({
  enqueueLocalMutation: vi.fn(),
}));

import { normalizeBrief } from "../../src/lib/brief.js";
import {
  budgetLabel,
  buildLanes,
  columnFor,
  foldSuggestions,
  hiddenChildren,
  laneCells,
  overBudget,
  progressSegments,
  trayRows,
} from "../../src/lib/swimlanes.js";
import {
  removeGroupedStep,
  ungroupPlanKey,
} from "../../src/lib/groupingPlan.js";
import {
  groupingDraft,
  groupingRequest,
  groupingTitle,
} from "../../src/lib/workGrouping.js";

const fixture = JSON.parse(
  readFileSync(
    new URL("../../../contracts/fixtures/brief/brief.v1.json", import.meta.url),
    "utf8",
  ),
);

function initiative(ref, extra = {}) {
  return {
    ref,
    title: ref,
    state: "moving",
    placement: "in_progress",
    parent_ref: null,
    rollup: {
      total: 0,
      by_state: {},
      past_promise: 0,
      needing_viewer: 0,
      workstreams: [],
      children: [],
      has_more: false,
    },
    ...extra,
  };
}

const child = (ref, state, extra = {}) => ({
  ref,
  title: ref,
  step_id: ref.replace("card:", ""),
  state,
  past_promise: false,
  needs_viewer: false,
  ...extra,
});

describe("brief normalization for the board", () => {
  it("keeps the rollup, parent, loops, budget and next actions", () => {
    const brief = normalizeBrief({
      ...fixture,
      initiatives: [
        initiative("card:capture", {
          rollup: {
            total: 3,
            by_state: { in_progress: 2, done: 1, weird: 0 },
            past_promise: 1,
            needing_viewer: 1,
            workstreams: [child("card:custody", "in_progress")],
            children: [
              child("card:custody", "in_progress", { past_promise: true }),
              child("card:a1", "blocked", { needs_viewer: true }),
            ],
            has_more: true,
          },
        }),
      ],
      loops: [initiative("card:signup-loop", { loop: { health: "healthy" } })],
      initiative_budget: { count: 17, target: 15, truncated: true },
      next_actions: [
        {
          label: "Choose a home",
          argv: ["anx", "work", "fold", "card:p76", "--into", "card:capture"],
          reasons: ["title_family"],
        },
        { label: "not runnable" },
      ],
    });
    const [lane] = brief.initiatives;
    expect(lane.parentRef).toBeNull();
    expect(lane.rollup).toMatchObject({
      total: 3,
      byState: { in_progress: 2, done: 1 },
      pastPromise: 1,
      needingViewer: 1,
      hasMore: true,
    });
    expect(lane.rollup.children[1]).toEqual({
      ref: "card:a1",
      title: "card:a1",
      stepId: "a1",
      state: "blocked",
      pastPromise: false,
      needsViewer: true,
    });
    expect(lane.rollup.workstreams.map((row) => row.ref)).toEqual([
      "card:custody",
    ]);
    expect(brief.loops.map((row) => row.ref)).toEqual(["card:signup-loop"]);
    expect(brief.initiativeBudget).toEqual({
      count: 17,
      target: 15,
      truncated: true,
    });
    expect(brief.nextActions).toHaveLength(1);
  });

  it("degrades to empty rollups and no budget from an older core", () => {
    const brief = normalizeBrief({
      initiatives: [{ ref: "card:old", title: "Old" }],
    });
    expect(brief.initiatives[0].rollup).toEqual({
      total: 0,
      byState: {},
      pastPromise: 0,
      needingViewer: 0,
      children: [],
      workstreams: [],
      hasMore: false,
    });
    expect(brief.initiativeBudget).toBeNull();
    expect(brief.loops).toEqual([]);
    expect(brief.nextActions).toEqual([]);
  });
});

describe("lanes", () => {
  const lanes = (rows) =>
    buildLanes(normalizeBrief({ initiatives: rows }).initiatives).map(
      (row) => row.ref,
    );

  it("puts needs-you first, then past promise, then core order, done last", () => {
    expect(
      lanes([
        initiative("card:done", { state: "done" }),
        initiative("card:quiet"),
        initiative("card:late", { state: "past_promise" }),
        initiative("card:asks", {
          rollup: { ...initiative("x").rollup, needing_viewer: 2 },
        }),
        initiative("card:quiet-2"),
      ]),
    ).toEqual([
      "card:asks",
      "card:late",
      "card:quiet",
      "card:quiet-2",
      "card:done",
    ]);
  });

  it("never turns a child or a loop into a lane", () => {
    expect(
      lanes([
        initiative("card:root"),
        initiative("card:child", { parent_ref: "card:root" }),
        initiative("card:loop", { loop: { health: "healthy" } }),
      ]),
    ).toEqual(["card:root"]);
  });

  it("places children by phase, and a child waiting on the reader in Blocked", () => {
    const rollup = normalizeBrief({
      initiatives: [
        initiative("card:r", {
          rollup: {
            ...initiative("x").rollup,
            total: 6,
            children: [
              child("card:backlog", "backlog"),
              child("card:review", "review"),
              child("card:stuck", "blocked"),
              child("card:ask", "in_progress", { needs_viewer: true }),
              child("card:cancelled", "cancelled"),
              child("card:new-phase", "triage"),
            ],
          },
        }),
      ],
    }).initiatives[0].rollup;
    const cells = laneCells(rollup);
    expect(
      Object.fromEntries(
        Object.entries(cells).map(([key, rows]) => [
          key,
          rows.map((r) => r.ref),
        ]),
      ),
    ).toEqual({
      not_started: ["card:backlog", "card:new-phase"],
      moving: ["card:review"],
      stuck: ["card:ask", "card:stuck"],
      done: ["card:cancelled"],
    });
    expect(columnFor({ state: "done", needsViewer: true })).toBe("stuck");
    expect(hiddenChildren(rollup)).toBe(0);
    expect(hiddenChildren({ ...rollup, total: 9 })).toBe(3);
  });

  it("sizes the progress bar from by_state, done first", () => {
    expect(
      progressSegments({ backlog: 1, ready: 1, in_progress: 1, done: 1 }),
    ).toEqual([
      { key: "done", count: 1, share: 0.25 },
      { key: "moving", count: 1, share: 0.25 },
      { key: "not_started", count: 2, share: 0.5 },
    ]);
    expect(progressSegments({})).toEqual([]);
  });
});

describe("tray and budget", () => {
  it("keeps tray rows that have no parent and drops self-homes", () => {
    expect(
      trayRows({
        work: [
          {
            ref: "card:a1-f4",
            title: "A1 F4",
            phase: "backlog",
            parent_ref: null,
            created_at: "2026-10-24T00:00:00Z",
            likely_home: [
              {
                ref: "card:a1",
                title: "A1 routing",
                reasons: ["title_family"],
              },
              { ref: "card:a1-f4", title: "self", reasons: [] },
            ],
          },
          { ref: "card:grouped", parent_ref: "card:a1" },
          { title: "no ref" },
        ],
      }),
    ).toEqual([
      {
        ref: "card:a1-f4",
        title: "A1 F4",
        phase: "backlog",
        createdAt: "2026-10-24T00:00:00Z",
        homes: [
          { ref: "card:a1", title: "A1 routing", reasons: ["title_family"] },
        ],
      },
    ]);
  });

  it("reads fold suggestions only from fold argv", () => {
    const next = [
      {
        argv: ["anx", "work", "fold", "card:p76", "--into", "card:margin"],
        reasons: ["title_family", "stale_top_level"],
      },
      { argv: ["anx", "work", "fold", "card:p76", "--into", "card:other"] },
      { argv: ["anx", "work", "group", "card:x", "--under", "card:margin"] },
      { argv: ["anx", "work", "fold", "card:self", "--into", "card:self"] },
      { argv: ["rm", "work", "fold", "card:y", "--into", "card:margin"] },
    ];
    expect(
      foldSuggestions(next, [{ ref: "card:margin", title: "Margin-50" }]),
    ).toEqual([
      {
        ref: "card:p76",
        title: "card:p76",
        into: "card:margin",
        intoTitle: "Margin-50",
        reason: "same title prefix, quiet for a week",
      },
    ]);
  });

  it("labels the budget and says when it is over", () => {
    expect(budgetLabel({ count: 6, target: 15, truncated: false })).toBe(
      "Initiatives 6/15",
    );
    expect(budgetLabel({ count: 17, target: 15, truncated: true })).toBe(
      "Initiatives 17+/15",
    );
    expect(overBudget({ count: 15, target: 15 })).toBe(false);
    expect(overBudget({ count: 16, target: 15 })).toBe(true);
    expect(budgetLabel(null)).toBe("");
  });
});

describe("grouping writes", () => {
  const work = { ref: "card:a1-f4", title: "A1 F4" };
  const parent = { ref: "card:a1", title: "A1 routing" };

  it("freezes the intent and binds actor and key", () => {
    const draft = groupingDraft("group", work, parent);
    parent.title = "Renamed later";
    expect(groupingRequest(draft, "human", "k1")).toEqual({
      operation: "work.group",
      request: {
        ref: "card:a1-f4",
        body: { actor_id: "human", request_key: "k1", under: "card:a1" },
      },
    });
    expect(groupingTitle(draft)).toBe("Group “A1 F4” under “A1 routing”");
    parent.title = "A1 routing";
    expect(
      groupingRequest(groupingDraft("fold", work, parent), "h", "k").request
        .body.into,
    ).toBe("card:a1");
    expect(
      groupingRequest(groupingDraft("unfold", work), "h", "k").request,
    ).toEqual({
      ref: "card:a1-f4",
      body: { actor_id: "h", request_key: "k" },
    });
    expect(
      groupingRequest(groupingDraft("ungroup", work, parent), "h", "k"),
    ).toEqual({
      operation: "work.ungroup",
      request: {
        ref: "card:a1-f4",
        parent: "card:a1",
        body: { actor_id: "h", request_key: "k" },
      },
    });
  });

  it("refuses a group with no home", () => {
    expect(() =>
      groupingRequest(groupingDraft("group", work), "h", "k"),
    ).toThrow(/initiative/);
  });

  it("removes only the grouped step and finds a landed removal unchanged", () => {
    const plan = {
      steps: [
        { id: "design", title: "Design", after: [] },
        { id: "card-1", title: "A1 F4", ref: "card:a1-f4", after: [] },
      ],
    };
    const next = removeGroupedStep(plan, "card:a1-f4");
    expect(next).toEqual({
      changed: true,
      plan: { steps: [{ id: "design", title: "Design", after: [] }] },
    });
    expect(removeGroupedStep(next.plan, "card:a1-f4").changed).toBe(false);
  });

  it("leaves a step another step now depends on", () => {
    expect(() =>
      removeGroupedStep(
        {
          steps: [
            { id: "card-1", ref: "card:a1-f4", after: [] },
            { id: "ship", after: ["card-1"] },
          ],
        },
        "card:a1-f4",
      ),
    ).toThrow(/depends/);
    expect(() => removeGroupedStep(null, "card:x")).toThrow(/incomplete/);
  });

  it("keys each plan write by the plan it was computed from", () => {
    expect(ungroupPlanKey("k", "2026-10-24T00:00:00Z")).toBe(
      "k:2026-10-24T00:00:00Z",
    );
  });
});
