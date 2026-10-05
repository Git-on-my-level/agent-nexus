import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { planLayout } from "../../src/lib/planShape.js";
import { indexResolvedRefs, refChipModel } from "../../src/lib/refResolve.js";

/**
 * The web UI no longer computes plan meaning — core does, and this checks the
 * UI renders what core computed without quietly changing it.
 *
 * The same corpus `core/internal/plans/plans_test.go` reads is replayed here:
 * each case's expected shape, critical path, next steps, progress and health
 * are fed in as `plan_state`, exactly as the server would send them, and the
 * layout has to surface them unaltered. An earlier version of this file ran a
 * JavaScript port of `plans.Compute` against the same corpus; the port drifted
 * from core in six places that the corpus did not happen to cover, so the port
 * is gone and this now guards the thing that remains: faithful rendering.
 */
const fixturePath = (name) =>
  fileURLToPath(
    new URL(
      `../../../contracts/fixtures/initiative-plans/${name}`,
      import.meta.url,
    ),
  );

const graphs = JSON.parse(readFileSync(fixturePath("graphs.json"), "utf8"));
const refFixture = JSON.parse(readFileSync(fixturePath("refs.json"), "utf8"));

/** The corpus case, as core's `plan_state` would arrive on the wire. */
function planStateFor(graph) {
  const statuses = Object.fromEntries(
    graph.plan.steps.map((step) => [step.id, step.status ?? "not_started"]),
  );
  for (const [ref, phase] of Object.entries(graph.facts ?? {})) {
    for (const step of graph.plan.steps) {
      // Core maps a known workflow state onto the step; the corpus states the
      // outcome, so the fixture carries core's answer rather than deriving one.
      if (step.ref === ref && phase === "done") statuses[step.id] = "done";
    }
  }
  return {
    steps: graph.plan.steps.map((step) => ({
      id: step.id,
      status: statuses[step.id],
      resolvable: Boolean(step.ref),
    })),
    progress: { done: graph.done, total: graph.plan.steps.length },
    critical_path: graph.critical_path,
    next_steps: graph.next_steps,
    shape: graph.shape,
    health: graph.health,
    last_movement_at: "2026-10-04T12:00:00Z",
  };
}

describe("plan rendering replays the shared contract corpus", () => {
  it("covers every graph fixture core checks", () => {
    expect(graphs.length).toBeGreaterThan(0);
    expect(graphs.map((graph) => graph.name)).toEqual(
      expect.arrayContaining(["chain", "branch-and-merge", "lanes", "empty"]),
    );
  });

  for (const graph of graphs) {
    describe(graph.name, () => {
      const layout = () =>
        planLayout(graph.plan, { planState: planStateFor(graph) });

      it("renders the shape core computed", () => {
        expect(layout().shape).toBe(graph.shape);
      });

      it("renders the critical path core computed", () => {
        expect(layout().criticalPath).toEqual(graph.critical_path);
      });

      it("renders the next steps core computed, in core's order", () => {
        const byId = new Map(
          graph.plan.steps.map((step) => [step.id, step.title]),
        );
        expect(layout().next).toEqual(
          graph.next_steps.map((id) => byId.get(id)),
        );
      });

      it("renders the progress core computed", () => {
        expect(layout().progress).toEqual({
          done: graph.done,
          total: graph.plan.steps.length,
        });
      });

      it("renders the health core computed", () => {
        expect(layout().health).toBe(graph.health);
      });

      it("gives every step the status core computed", () => {
        const state = planStateFor(graph);
        const byId = new Map(
          layout().nodes.map((node) => [node.id, node.status]),
        );
        for (const step of state.steps) {
          expect(byId.get(step.id)).toBe(step.status);
        }
      });
    });
  }
});

describe("a plan with no computed state claims nothing", () => {
  const plan = {
    steps: [
      { id: "a", title: "A", after: [], status: "done" },
      { id: "b", title: "B", after: ["a"], status: "blocked" },
    ],
  };

  it("shows no shape, health, progress, path or next steps", () => {
    const layout = planLayout(plan);
    expect(layout.shape).toBe("");
    expect(layout.health).toBe("");
    expect(layout.progress).toBeNull();
    expect(layout.criticalPath).toEqual([]);
    expect(layout.next).toEqual([]);
    expect(layout.hasState).toBe(false);
  });

  it("does not take a status from the authored plan", () => {
    // `status` on a step is core's fallback input, not an answer: core decides
    // whether a ref overrides it. Rendering it here would show "done" for a
    // step core would have called not_started.
    expect(planLayout(plan).nodes.map((node) => node.status)).toEqual(["", ""]);
  });

  it("still lays the steps out, because geometry is not semantics", () => {
    const layout = planLayout(plan);
    expect(layout.layers).toEqual([["a"], ["b"]]);
    expect(layout.edges).toEqual([
      { from: "a", to: "b", onCriticalPath: false },
    ]);
  });
});

describe("plan_state fields that do not belong to the contract", () => {
  const plan = { steps: [{ id: "a", title: "A", after: [] }] };

  it("ignores a shape or health that is not a contract value", () => {
    const layout = planLayout(plan, {
      planState: { shape: "spiral", health: "fine" },
    });
    expect(layout.shape).toBe("");
    expect(layout.health).toBe("");
  });

  it("drops a step id the authored plan does not contain", () => {
    const layout = planLayout(plan, {
      planState: { critical_path: ["a", "ghost"], next_steps: ["ghost"] },
    });
    expect(layout.criticalPath).toEqual(["a"]);
    expect(layout.next).toEqual([]);
  });
});

describe("batch ref resolve matches the shared ref corpus", () => {
  const items = refFixture.refs.map((ref, index) => ({
    ref,
    kind: ref.includes(":") ? ref.split(":")[0] : "",
    title: refFixture.resolvable[index] ? ref : "",
    resolvable: refFixture.resolvable[index],
  }));
  const resolved = indexResolvedRefs({ items }, refFixture.refs);
  const context = { organizationSlug: "scaling", workspaceSlug: "anx" };

  it("keeps every ref the corpus lists, resolvable or not", () => {
    for (const ref of refFixture.refs) {
      expect(resolved.has(ref)).toBe(true);
    }
  });

  it("agrees with the corpus on what resolves", () => {
    for (const [index, ref] of refFixture.refs.entries()) {
      expect(refChipModel(ref, resolved, context).resolvable).toBe(
        refFixture.resolvable[index],
      );
    }
  });

  it("renders the unknown ref as not found rather than dropping it", () => {
    const missing = refChipModel("card:missing", resolved, context);
    expect(missing.resolvable).toBe(false);
    expect(missing.title).toBe("card:missing");
    expect(missing.href).toBe("");
  });

  it("handles the duplicated ref the corpus repeats", () => {
    expect(
      refFixture.refs.filter((ref) => ref === "card:initiative"),
    ).toHaveLength(2);
    expect(refChipModel("card:initiative", resolved, context).resolvable).toBe(
      true,
    );
  });
});
