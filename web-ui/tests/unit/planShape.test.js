import { describe, expect, it } from "vitest";

import {
  PLAN_COLLAPSE_MIN,
  layerSteps,
  normalizePlanSteps,
  planComponents,
  planLayout,
  planTreeGeometry,
  todayMarkerIndex,
} from "../../src/lib/planShape.js";

/** `after` defaults to empty so each case states only the edges it cares about. */
const step = (id, after = [], extra = {}) => ({
  id,
  title: id,
  after,
  ...extra,
});

const plan = (steps) => ({ steps });

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
  it("groups disconnected runs, whatever shape core computed", () => {
    // Lanes are structure, not semantics: who is connected to whom is readable
    // from the steps themselves, so it stays here. The *name* of the shape is
    // core's to give.
    const layout = planLayout(
      plan([step("a"), step("b", ["a"]), step("x"), step("y", ["x"])]),
      { planState: { shape: "lanes" } },
    );
    expect(layout.shape).toBe("lanes");
    expect(layout.lanes).toEqual([
      ["a", "b"],
      ["x", "y"],
    ]);
  });

  it("reports no shape of its own when core computed none", () => {
    const layout = planLayout(plan([step("a"), step("b", ["a"])]));
    expect(layout.shape).toBe("");
    expect(layout.health).toBe("");
    expect(layout.progress).toBeNull();
    expect(layout.hasState).toBe(false);
  });
});

describe("planTreeGeometry", () => {
  const branching = () =>
    planLayout(plan([step("a"), step("b", ["a"]), step("c", ["a"])]), {
      planState: {
        shape: "dag",
        critical_path: ["a", "b"],
        steps: [
          { id: "a", status: "not_started" },
          { id: "b", status: "not_started" },
          { id: "c", status: "not_started" },
        ],
      },
    });

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

describe("planTreeGeometry folds the finished prefix", () => {
  /** Core computes status; these cases state what it said. */
  const stateFor = (steps, overrides = {}) => ({
    steps,
    shape: "dag",
    progress: { done: 0, total: steps.length },
    critical_path: [],
    next_steps: [],
    ...overrides,
  });

  const chain = (ids) =>
    plan(ids.map((id, index) => step(id, index ? [ids[index - 1]] : [])));

  it("folds a leading run of done layers into one summary column", () => {
    const ids = ["a", "b", "c", "d"];
    const layout = planLayout(chain(ids), {
      planState: stateFor([
        { id: "a", status: "done" },
        { id: "b", status: "done" },
        { id: "c", status: "active" },
        { id: "d", status: "not_started" },
      ]),
    });
    const geometry = planTreeGeometry(layout);
    expect(geometry.collapsed).toMatchObject({ count: 2 });
    expect(geometry.nodes.map((node) => node.id)).toEqual(["c", "d"]);
  });

  it("does not fold a single done step, which saves nothing", () => {
    const layout = planLayout(chain(["a", "b", "c"]), {
      planState: stateFor([
        { id: "a", status: "done" },
        { id: "b", status: "active" },
        { id: "c", status: "not_started" },
      ]),
    });
    expect(PLAN_COLLAPSE_MIN).toBe(2);
    expect(planTreeGeometry(layout).collapsed).toBeNull();
    expect(planTreeGeometry(layout).nodes).toHaveLength(3);
  });

  it("does not fold a done layer that sits between live ones", () => {
    // Hiding a middle layer would break the reader's sense of what depends on
    // what, so only a leading run folds.
    const layout = planLayout(chain(["a", "b", "c", "d"]), {
      planState: stateFor([
        { id: "a", status: "active" },
        { id: "b", status: "done" },
        { id: "c", status: "done" },
        { id: "d", status: "not_started" },
      ]),
    });
    expect(planTreeGeometry(layout).collapsed).toBeNull();
  });

  it("draws a finished plan in full rather than folding it to nothing", () => {
    const layout = planLayout(chain(["a", "b", "c"]), {
      planState: stateFor([
        { id: "a", status: "done" },
        { id: "b", status: "done" },
        { id: "c", status: "done" },
      ]),
    });
    const geometry = planTreeGeometry(layout);
    expect(geometry.collapsed).toBeNull();
    expect(geometry.nodes).toHaveLength(3);
  });

  it("keeps a dependency on folded work visible as an edge from the summary", () => {
    const layout = planLayout(chain(["a", "b", "c", "d"]), {
      planState: stateFor([
        { id: "a", status: "done" },
        { id: "b", status: "done" },
        { id: "c", status: "active" },
        { id: "d", status: "not_started" },
      ]),
    });
    const geometry = planTreeGeometry(layout);
    const fromFolded = geometry.edges.filter((edge) => edge.fromCollapsed);
    expect(fromFolded.map((edge) => edge.to)).toEqual(["c"]);
    // No edge points into the folded prefix: there is nothing there to hit.
    expect(geometry.edges.some((edge) => edge.to === "b")).toBe(false);
  });

  it("can be turned off", () => {
    const layout = planLayout(chain(["a", "b", "c"]), {
      planState: stateFor([
        { id: "a", status: "done" },
        { id: "b", status: "done" },
        { id: "c", status: "active" },
      ]),
    });
    expect(
      planTreeGeometry(layout, { collapseDone: false }).collapsed,
    ).toBeNull();
  });
});

describe("planTreeGeometry fits the width it is given", () => {
  const wide = (count) =>
    plan(
      Array.from({ length: count }, (_, index) =>
        step(`s${index}`, index ? [`s${index - 1}`] : []),
      ),
    );
  const layoutFor = (count) =>
    planLayout(wide(count), {
      planState: {
        steps: Array.from({ length: count }, (_, index) => ({
          id: `s${index}`,
          status: "not_started",
        })),
        shape: "dag",
        progress: { done: 0, total: count },
        critical_path: [],
        next_steps: [],
      },
    });

  it("uses its preferred size when there is room", () => {
    const geometry = planTreeGeometry(layoutFor(3), { availableWidth: 4000 });
    expect(geometry.nodes[0].width).toBe(176);
    expect(geometry.width).toBeLessThanOrEqual(4000);
  });

  it("shrinks gaps, then nodes, so the remaining steps fit on screen", () => {
    const roomy = planTreeGeometry(layoutFor(6), { availableWidth: 4000 });
    const tight = planTreeGeometry(layoutFor(6), { availableWidth: 900 });
    expect(tight.width).toBeLessThanOrEqual(900);
    expect(tight.nodes[0].width).toBeLessThan(roomy.nodes[0].width);
    // Still legible: the floor is a width a step title can be read in.
    expect(tight.nodes[0].width).toBeGreaterThanOrEqual(108);
  });

  it("scrolls rather than shrinking past legibility", () => {
    // 20 columns in 400px cannot fit; the region scrolls, which is the one
    // deliberate sideways scroller on the page.
    const geometry = planTreeGeometry(layoutFor(20), { availableWidth: 400 });
    expect(geometry.width).toBeGreaterThan(400);
    expect(geometry.nodes[0].width).toBe(108);
  });

  it("lays out at its preferred size when the width is not known yet", () => {
    // Server render, or before the first measurement.
    const geometry = planTreeGeometry(layoutFor(4), { availableWidth: 0 });
    expect(geometry.nodes[0].width).toBe(176);
  });
});
