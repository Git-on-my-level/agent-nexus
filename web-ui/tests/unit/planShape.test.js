import { describe, expect, it } from "vitest";

import {
  classifyPlanShape,
  planComponents,
  planTreeGeometry,
  todayMarkerIndex,
  criticalPath,
  DEFAULT_STALLED_DAYS,
  effectiveStepStatus,
  layerSteps,
  nextActionableSteps,
  normalizePlanSteps,
  planHealth,
  planLayout,
} from "../../src/lib/planShape.js";

/** `after` defaults to empty so each case states only the edges it cares about. */
const step = (id, after = [], extra = {}) => ({
  id,
  title: id,
  after,
  ...extra,
});

const plan = (steps) => ({ steps });
const statuses = (entries) => Object.fromEntries(entries);

describe("normalizePlanSteps", () => {
  it("keeps steps and reports an after that names no step", () => {
    const { steps, issues } = normalizePlanSteps(
      plan([step("a"), step("b", ["ghost"])]),
    );
    expect(steps.map((entry) => entry.id)).toEqual(["a", "b"]);
    expect(steps[1].after).toEqual([]);
    expect(issues).toEqual([
      'Step "b" depends on "ghost", which is not in the plan.',
    ]);
  });

  it("drops a self-dependency without complaining about a missing step", () => {
    const { steps, issues } = normalizePlanSteps(plan([step("a", ["a"])]));
    expect(steps[0].after).toEqual([]);
    expect(issues).toEqual([]);
  });

  it("keeps the first of a duplicated id", () => {
    const { steps, issues } = normalizePlanSteps(
      plan([
        { id: "a", title: "first", after: [] },
        { id: "a", title: "second", after: [] },
      ]),
    );
    expect(steps).toHaveLength(1);
    expect(steps[0].title).toBe("first");
    expect(issues).toEqual(['Duplicate step id "a" was skipped.']);
  });

  it("falls back to the id when a step has no title", () => {
    const { steps } = normalizePlanSteps(plan([{ id: "ship-it", after: [] }]));
    expect(steps[0].title).toBe("ship-it");
  });

  it("reports a repeated fault once", () => {
    const { issues } = normalizePlanSteps(
      plan([{ after: [] }, { after: [] }, { after: [] }]),
    );
    expect(issues).toEqual(["A step without an id was skipped."]);
  });

  it("treats a missing plan as empty", () => {
    expect(normalizePlanSteps(null).steps).toEqual([]);
    expect(normalizePlanSteps({}).steps).toEqual([]);
  });

  it("dedupes a repeated dependency", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a", "a"])]),
    );
    expect(steps[1].after).toEqual(["a"]);
  });
});

describe("classifyPlanShape", () => {
  it("classifies a single path as a chain", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("c", ["b"])]),
    );
    expect(classifyPlanShape(steps)).toBe("chain");
  });

  it("classifies a flat list with no dependencies as lanes", () => {
    // Every step is its own root, and core calls more than one root `lanes`.
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b"), step("c")]),
    );
    expect(classifyPlanShape(steps)).toBe("lanes");
  });

  it("classifies a single step as a chain", () => {
    const { steps } = normalizePlanSteps(plan([step("only")]));
    expect(classifyPlanShape(steps)).toBe("chain");
  });

  it("classifies a branch as a dag", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("c", ["a"])]),
    );
    expect(classifyPlanShape(steps)).toBe("dag");
  });

  it("classifies a merge as a dag", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b"), step("c", ["a", "b"])]),
    );
    expect(classifyPlanShape(steps)).toBe("dag");
  });

  it("turns a timeline into a tree when one step adds an after", () => {
    const chain = [step("a"), step("b", ["a"]), step("c", ["b"])];
    expect(classifyPlanShape(normalizePlanSteps(plan(chain)).steps)).toBe(
      "chain",
    );
    const branched = [...chain, step("d", ["a"])];
    expect(classifyPlanShape(normalizePlanSteps(plan(branched)).steps)).toBe(
      "dag",
    );
  });

  it("classifies two parallel chains as lanes", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("x"), step("y", ["x"])]),
    );
    expect(classifyPlanShape(steps)).toBe("lanes");
  });

  it("classifies a linked chain beside a lone step as lanes", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("solo")]),
    );
    expect(classifyPlanShape(steps)).toBe("lanes");
  });

  it("prefers dag when one lane branches", () => {
    const { steps } = normalizePlanSteps(
      plan([
        step("a"),
        step("b", ["a"]),
        step("c", ["a"]),
        step("x"),
        step("y", ["x"]),
      ]),
    );
    expect(classifyPlanShape(steps)).toBe("dag");
  });

  it("treats an empty plan as a chain", () => {
    expect(classifyPlanShape([])).toBe("chain");
  });

  it("classifies a dependency loop as a dag, not a chain beside the rest", () => {
    // Each step in a two-step loop has exactly one dependency, so the degree
    // test alone would call this a chain and the loop would never be parked.
    const { steps } = normalizePlanSteps(
      plan([step("ok"), step("a", ["b"]), step("b", ["a"])]),
    );
    expect(classifyPlanShape(steps)).toBe("dag");
  });
});

describe("layerSteps", () => {
  it("puts a step one layer after its latest dependency", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("c", ["a", "b"])]),
    );
    expect(layerSteps(steps).layers).toEqual([["a"], ["b"], ["c"]]);
  });

  it("puts independent steps in the same layer, in declaration order", () => {
    const { steps } = normalizePlanSteps(
      plan([step("b"), step("a"), step("c", ["a", "b"])]),
    );
    expect(layerSteps(steps).layers).toEqual([["b", "a"], ["c"]]);
  });

  it("returns cyclic steps instead of looping forever", () => {
    // normalizePlanSteps keeps both edges: each id exists, so neither is dropped.
    const { steps } = normalizePlanSteps(
      plan([step("a", ["b"]), step("b", ["a"]), step("c")]),
    );
    const { layers, cyclic } = layerSteps(steps);
    expect(layers).toEqual([["c"]]);
    expect(cyclic).toEqual(["a", "b"]);
  });

  it("layers a plan with no edges into one layer", () => {
    const { steps } = normalizePlanSteps(plan([step("a"), step("b")]));
    expect(layerSteps(steps).layers).toEqual([["a", "b"]]);
  });
});

describe("effectiveStepStatus", () => {
  it("derives status from a resolved ref phase", () => {
    const resolved = { "card:ship": { phase: "in_progress" } };
    expect(effectiveStepStatus({ ref: "card:ship" }, resolved)).toBe("active");
  });

  it("maps the workflow states core calls finished", () => {
    for (const phase of ["done", "published", "closed", "resolved", "merged"]) {
      expect(
        effectiveStepStatus({ ref: "card:a" }, { "card:a": { phase } }),
      ).toBe("done");
    }
  });

  it("maps review and in_progress to active", () => {
    for (const phase of ["review", "in_progress", "active"]) {
      expect(
        effectiveStepStatus({ ref: "card:a" }, { "card:a": { phase } }),
      ).toBe("active");
    }
  });

  it("treats a cancelled card as not started, as core does", () => {
    // Core's mapping names the states that count as finished work, and a
    // cancelled card is not finished work; everything unlisted falls through.
    expect(
      effectiveStepStatus(
        { ref: "card:a" },
        { "card:a": { phase: "cancelled" } },
      ),
    ).toBe("not_started");
  });

  it("reads a Map as well as a plain lookup", () => {
    const asMap = new Map([["card:a", { phase: "done" }]]);
    expect(effectiveStepStatus({ ref: "card:a" }, asMap)).toBe("done");
  });

  it("lets a resolved ref override an authored status", () => {
    const resolved = { "card:a": { phase: "done" } };
    expect(
      effectiveStepStatus({ ref: "card:a", status: "not_started" }, resolved),
    ).toBe("done");
  });

  it("falls back to the authored status when the ref does not resolve", () => {
    expect(
      effectiveStepStatus({ ref: "card:gone", status: "blocked" }, {}),
    ).toBe("blocked");
  });

  it("uses the authored status when there is no ref", () => {
    expect(effectiveStepStatus({ status: "active" })).toBe("active");
  });

  it("defaults to not_started for an unknown authored status", () => {
    expect(effectiveStepStatus({ status: "nearly" })).toBe("not_started");
    expect(effectiveStepStatus({})).toBe("not_started");
  });

  it("accepts a step status sent directly on the resolved ref", () => {
    expect(
      effectiveStepStatus(
        { ref: "card:a" },
        { "card:a": { status: "blocked" } },
      ),
    ).toBe("blocked");
  });
});

describe("criticalPath", () => {
  it("returns the longest remaining chain", () => {
    const { steps } = normalizePlanSteps(
      plan([
        step("a"),
        step("b", ["a"]),
        step("c", ["b"]),
        step("shortcut", ["a"]),
      ]),
    );
    const statusById = statuses([
      ["a", "not_started"],
      ["b", "not_started"],
      ["c", "not_started"],
      ["shortcut", "not_started"],
    ]);
    expect(criticalPath(steps, statusById)).toEqual(["a", "b", "c"]);
  });

  it("ignores done steps so the path is what is left to do", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("c", ["b"])]),
    );
    const statusById = statuses([
      ["a", "done"],
      ["b", "done"],
      ["c", "active"],
    ]);
    expect(criticalPath(steps, statusById)).toEqual(["c"]);
  });

  it("returns nothing when every step is done", () => {
    const { steps } = normalizePlanSteps(plan([step("a"), step("b", ["a"])]));
    expect(
      criticalPath(
        steps,
        statuses([
          ["a", "done"],
          ["b", "done"],
        ]),
      ),
    ).toEqual([]);
  });

  it("is stable when two remaining chains tie on length", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("x"), step("y", ["x"])]),
    );
    const statusById = statuses([
      ["a", "not_started"],
      ["b", "not_started"],
      ["x", "not_started"],
      ["y", "not_started"],
    ]);
    expect(criticalPath(steps, statusById)).toEqual(["a", "b"]);
    expect(criticalPath(steps, statusById)).toEqual(["a", "b"]);
  });
});

describe("nextActionableSteps", () => {
  it("returns steps whose dependencies are all done", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("c", ["b"])]),
    );
    expect(
      nextActionableSteps(
        steps,
        statuses([
          ["a", "done"],
          ["b", "not_started"],
          ["c", "not_started"],
        ]),
      ),
    ).toEqual(["b"]);
  });

  it("returns every root when nothing has started", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("x"), step("b", ["a"])]),
    );
    expect(
      nextActionableSteps(
        steps,
        statuses([
          ["a", "not_started"],
          ["x", "not_started"],
          ["b", "not_started"],
        ]),
      ),
    ).toEqual(["a", "x"]);
  });
});

describe("planHealth", () => {
  const now = Date.parse("2026-10-04T12:00:00Z");
  const daysAgo = (days) => new Date(now - days * 86_400_000).toISOString();
  /** Health reads the whole graph, so cases state their steps, not a path. */
  const chain = normalizePlanSteps(plan([step("a"), step("b", ["a"])])).steps;
  const lanes = normalizePlanSteps(
    plan([step("a"), step("b", ["a"]), step("c"), step("d", ["c"])]),
  ).steps;

  it("is blocked when a blocked step sits on a longest remaining path", () => {
    expect(
      planHealth({
        steps: chain,
        statusById: { a: "blocked", b: "not_started" },
        lastMovedAt: daysAgo(0),
        now,
      }),
    ).toBe("blocked");
  });

  it("outranks stalled with blocked", () => {
    expect(
      planHealth({
        steps: chain,
        statusById: { a: "blocked", b: "not_started" },
        lastMovedAt: daysAgo(30),
        now,
      }),
    ).toBe("blocked");
  });

  it("is blocked from a lane that is not the highlighted path", () => {
    // Both lanes are two steps long, so only one wins the tie-break; a blocked
    // step on the other still blocks the plan.
    expect(
      planHealth({
        steps: lanes,
        statusById: {
          a: "not_started",
          b: "not_started",
          c: "blocked",
          d: "not_started",
        },
        lastMovedAt: daysAgo(0),
        now,
      }),
    ).toBe("blocked");
  });

  it("ignores a blocked step on a shorter path", () => {
    const steps = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("c", ["b"]), step("side")]),
    ).steps;
    expect(
      planHealth({
        steps,
        statusById: {
          a: "not_started",
          b: "not_started",
          c: "not_started",
          side: "blocked",
        },
        lastMovedAt: daysAgo(0),
        now,
      }),
    ).toBe("on_track");
  });

  it("is stalled at the threshold and on track just inside it", () => {
    const statusById = { a: "active", b: "not_started" };
    expect(
      planHealth({
        steps: chain,
        statusById,
        lastMovedAt: daysAgo(DEFAULT_STALLED_DAYS),
        now,
      }),
    ).toBe("stalled");
    expect(
      planHealth({
        steps: chain,
        statusById,
        lastMovedAt: daysAgo(DEFAULT_STALLED_DAYS - 1),
        now,
      }),
    ).toBe("on_track");
  });

  it("honours a configured threshold", () => {
    expect(
      planHealth({
        steps: chain,
        statusById: { a: "active", b: "not_started" },
        lastMovedAt: daysAgo(2),
        now,
        stalledDays: 1,
      }),
    ).toBe("stalled");
  });

  it("calls a finished plan on track however long ago it moved", () => {
    // Nothing is waiting, so nothing is stale.
    expect(
      planHealth({
        steps: chain,
        statusById: { a: "done", b: "done" },
        lastMovedAt: daysAgo(90),
        now,
      }),
    ).toBe("on_track");
  });

  it("calls an empty plan on track", () => {
    expect(
      planHealth({ steps: [], statusById: {}, lastMovedAt: daysAgo(90), now }),
    ).toBe("on_track");
  });

  it("does not call a plan stalled when movement is unknown", () => {
    const statusById = { a: "active", b: "not_started" };
    expect(
      planHealth({ steps: chain, statusById, lastMovedAt: null, now }),
    ).toBe("on_track");
    expect(
      planHealth({ steps: chain, statusById, lastMovedAt: "not a date", now }),
    ).toBe("on_track");
  });
});

describe("planLayout", () => {
  it("lays out a chain as a timeline with progress and the next step", () => {
    const layout = planLayout(
      plan([
        step("spec", [], { status: "done" }),
        step("build", ["spec"], { status: "active" }),
        step("ship", ["build"]),
      ]),
      {
        lastMovedAt: "2026-10-04T11:00:00Z",
        now: Date.parse("2026-10-04T12:00:00Z"),
      },
    );
    expect(layout.shape).toBe("chain");
    expect(layout.health).toBe("on_track");
    expect(layout.progress).toEqual({ done: 1, total: 3 });
    expect(layout.criticalPath).toEqual(["build", "ship"]);
    expect(layout.next).toEqual(["build"]);
    expect(layout.layers).toEqual([["spec"], ["build"], ["ship"]]);
  });

  it("marks only consecutive critical-path edges", () => {
    const layout = planLayout(
      plan([step("a"), step("b", ["a"]), step("side", ["a"])]),
    );
    expect(layout.criticalPath).toEqual(["a", "b"]);
    const edge = (from, to) =>
      layout.edges.find((entry) => entry.from === from && entry.to === to);
    expect(edge("a", "b").onCriticalPath).toBe(true);
    expect(edge("a", "side").onCriticalPath).toBe(false);
  });

  it("gives every node a layer and a row for the tree layout", () => {
    const layout = planLayout(
      plan([step("a"), step("b", ["a"]), step("c", ["a"])]),
    );
    expect(layout.shape).toBe("dag");
    const node = (id) => layout.nodes.find((entry) => entry.id === id);
    expect(node("a")).toMatchObject({ layer: 0, row: 0 });
    expect(node("b")).toMatchObject({ layer: 1, row: 0 });
    expect(node("c")).toMatchObject({ layer: 1, row: 1 });
  });

  it("derives step status from resolved refs", () => {
    const layout = planLayout(
      plan([step("a", [], { ref: "card:done-thing" }), step("b", ["a"])]),
      { resolved: { "card:done-thing": { phase: "done" } } },
    );
    expect(layout.progress).toEqual({ done: 1, total: 2 });
    expect(layout.nodes[0].status).toBe("done");
  });

  it("reports a dependency loop and still lays out the rest", () => {
    const layout = planLayout(
      plan([step("ok"), step("a", ["b"]), step("b", ["a"])]),
    );
    expect(layout.cyclic).toEqual(["a", "b"]);
    expect(layout.layers).toEqual([["ok"]]);
    expect(layout.issues).toContain(
      "2 steps depend on each other in a loop and are listed after the diagram.",
    );
  });

  it("prefers core's plan_state over the derived values", () => {
    // Fuller coverage of this lives in planContractConformance.test.js.
    const layout = planLayout(
      { steps: [step("a"), step("b", ["a"])] },
      { planState: { shape: "lanes", health: "stalled" } },
    );
    expect(layout.shape).toBe("lanes");
    expect(layout.health).toBe("stalled");
  });

  it("ignores a plan_state shape that is not a known shape", () => {
    const layout = planLayout(
      { steps: [step("a"), step("b", ["a"])] },
      { planState: { shape: "spiral" } },
    );
    expect(layout.shape).toBe("chain");
  });

  it("returns an empty layout for a card with no plan", () => {
    const layout = planLayout(null);
    expect(layout).toMatchObject({
      shape: "chain",
      nodes: [],
      edges: [],
      progress: { done: 0, total: 0 },
      next: [],
    });
  });
});

describe("planComponents", () => {
  it("groups linked steps and leaves separate runs apart", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b", ["a"]), step("x"), step("y", ["x"])]),
    );
    expect(planComponents(steps)).toEqual([
      ["a", "b"],
      ["x", "y"],
    ]);
  });

  it("treats a merge as one component", () => {
    const { steps } = normalizePlanSteps(
      plan([step("a"), step("b"), step("c", ["a", "b"])]),
    );
    expect(planComponents(steps)).toHaveLength(1);
  });

  it("gives each unlinked step its own group", () => {
    const { steps } = normalizePlanSteps(plan([step("a"), step("b")]));
    expect(planComponents(steps)).toEqual([["a"], ["b"]]);
  });
});

describe("planLayout lanes", () => {
  it("reports the lanes alongside the shape", () => {
    const layout = planLayout(
      plan([step("a"), step("b", ["a"]), step("x"), step("y", ["x"])]),
    );
    expect(layout.shape).toBe("lanes");
    expect(layout.lanes).toEqual([
      ["a", "b"],
      ["x", "y"],
    ]);
  });
});

describe("planTreeGeometry", () => {
  const branching = () =>
    planLayout(plan([step("a"), step("b", ["a"]), step("c", ["a"])]));

  it("places a node from its layer and row with nothing measured", () => {
    const geometry = planTreeGeometry(branching(), {
      nodeWidth: 100,
      nodeHeight: 50,
      gapX: 40,
      gapY: 10,
    });
    const at = (id) => geometry.nodes.find((node) => node.id === id);
    expect(at("a")).toMatchObject({ x: 0, y: 0, width: 100, height: 50 });
    expect(at("b")).toMatchObject({ x: 140, y: 0 });
    expect(at("c")).toMatchObject({ x: 140, y: 60 });
  });

  it("sizes the canvas to the widest layer and the longest chain", () => {
    const geometry = planTreeGeometry(branching(), {
      nodeWidth: 100,
      nodeHeight: 50,
      gapX: 40,
      gapY: 10,
    });
    // Two columns, two rows.
    expect(geometry.width).toBe(240);
    expect(geometry.height).toBe(110);
  });

  it("is deterministic for the same plan", () => {
    expect(planTreeGeometry(branching())).toEqual(
      planTreeGeometry(branching()),
    );
  });

  it("draws an edge between the boxes it joins and marks the critical one", () => {
    const geometry = planTreeGeometry(branching(), {
      nodeWidth: 100,
      nodeHeight: 50,
      gapX: 40,
      gapY: 10,
    });
    const edge = geometry.edges.find((entry) => entry.to === "b");
    // Leaves a's right edge at its vertical centre and enters b's left edge.
    expect(edge.path.startsWith("M 100 25 C")).toBe(true);
    expect(edge.path.endsWith("140 25")).toBe(true);
    expect(edge.onCriticalPath).toBe(true);
    expect(
      geometry.edges.find((entry) => entry.to === "c").onCriticalPath,
    ).toBe(false);
  });

  it("parks cyclic steps in a column of their own", () => {
    const layout = planLayout(
      plan([step("ok"), step("a", ["b"]), step("b", ["a"])]),
    );
    const geometry = planTreeGeometry(layout, {
      nodeWidth: 100,
      nodeHeight: 50,
      gapX: 40,
      gapY: 10,
    });
    const at = (id) => geometry.nodes.find((node) => node.id === id);
    expect(at("ok").x).toBe(0);
    expect(at("a").x).toBe(140);
    expect(at("b").x).toBe(140);
    expect(geometry.width).toBe(240);
  });

  it("returns an empty canvas for a plan with no steps", () => {
    expect(planTreeGeometry(planLayout(null))).toMatchObject({
      width: 0,
      height: 0,
      nodes: [],
      edges: [],
    });
  });
});

describe("todayMarkerIndex", () => {
  const now = Date.parse("2026-10-04T12:00:00Z");

  it("marks before the first step still to come", () => {
    const nodes = [
      { id: "a", due: "2026-10-01" },
      { id: "b", due: "2026-10-09" },
      { id: "c", due: "2026-10-20" },
    ];
    expect(todayMarkerIndex(nodes, now)).toBe(1);
  });

  it("marks at the end when every dated step has passed", () => {
    const nodes = [
      { id: "a", due: "2026-09-01" },
      { id: "b", due: "2026-09-20" },
    ];
    expect(todayMarkerIndex(nodes, now)).toBe(2);
  });

  it("marks at the start when nothing is due yet", () => {
    expect(todayMarkerIndex([{ id: "a", due: "2026-12-01" }], now)).toBe(0);
  });

  it("gives no marker when no step carries a date", () => {
    expect(todayMarkerIndex([{ id: "a" }, { id: "b", due: "" }], now)).toBe(-1);
    expect(todayMarkerIndex([], now)).toBe(-1);
  });

  it("ignores undated steps when deciding where today falls", () => {
    const nodes = [
      { id: "a", due: "2026-10-01" },
      { id: "undated" },
      { id: "c", due: "2026-10-20" },
    ];
    expect(todayMarkerIndex(nodes, now)).toBe(2);
  });
});
