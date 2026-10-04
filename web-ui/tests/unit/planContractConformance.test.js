import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { indexResolvedRefs, refChipModel } from "../../src/lib/refResolve.js";
import { planLayout } from "../../src/lib/planShape.js";

/**
 * The web UI derives plan shape, critical path, next steps and health locally
 * whenever it has no `plan_state` — a fixture, an unsaved edit, or a
 * `dependency-diagram` panel. That derivation is a port of `plans.Compute` in
 * core, and a port that drifts is worse than no port at all: the same plan
 * would render one way with the server's state and another without it.
 *
 * So both sides run the same corpus. `core/internal/plans/plans_test.go` reads
 * this file; so does this test.
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

/**
 * The fixtures give each ref's workflow state as `facts`. Core turns those into
 * `plans.Fact`; the UI gets the same information from batch ref resolve, so the
 * facts are fed in the shape `refResolve` produces.
 */
function resolvedFrom(facts = {}) {
  return indexResolvedRefs({
    items: Object.entries(facts).map(([ref, phase]) => ({
      ref,
      kind: ref.split(":")[0],
      title: ref,
      phase,
      status: phase,
      resolvable: true,
    })),
  });
}

describe("plan derivation matches the shared contract corpus", () => {
  it("covers every graph fixture core checks", () => {
    expect(graphs.length).toBeGreaterThan(0);
    expect(graphs.map((graph) => graph.name)).toEqual(
      expect.arrayContaining(["chain", "branch-and-merge", "lanes", "empty"]),
    );
  });

  for (const graph of graphs) {
    describe(graph.name, () => {
      const layout = () =>
        planLayout(graph.plan, {
          resolved: resolvedFrom(graph.facts),
          // Movement is "now" unless a case is about staleness, so these cases
          // exercise shape, path and blocking rather than the stall clock.
          lastMovedAt: new Date().toISOString(),
        });

      it("derives the contract shape", () => {
        expect(layout().shape).toBe(graph.shape);
      });

      it("derives the contract critical path", () => {
        expect(layout().criticalPath).toEqual(graph.critical_path);
      });

      it("derives the contract next steps", () => {
        const byTitle = new Map(
          graph.plan.steps.map((step) => [step.id, step.title]),
        );
        expect(layout().next).toEqual(
          graph.next_steps.map((id) => byTitle.get(id)),
        );
      });

      it("derives the contract progress", () => {
        expect(layout().progress).toEqual({
          done: graph.done,
          total: graph.plan.steps.length,
        });
      });

      it("derives the contract health", () => {
        expect(layout().health).toBe(graph.health);
      });
    });
  }
});

describe("plan_state from core wins over local derivation", () => {
  const plan = { steps: [{ id: "a", title: "A", after: [] }] };

  it("takes shape, health, progress, path and next steps as given", () => {
    const layout = planLayout(plan, {
      planState: {
        steps: [{ id: "a", status: "blocked" }],
        progress: { done: 0, total: 1 },
        critical_path: ["a"],
        next_steps: [],
        shape: "lanes",
        health: "blocked",
        last_movement_at: "2026-10-01T00:00:00Z",
      },
    });
    expect(layout.shape).toBe("lanes");
    expect(layout.health).toBe("blocked");
    expect(layout.criticalPath).toEqual(["a"]);
    expect(layout.next).toEqual([]);
    expect(layout.nodes[0].status).toBe("blocked");
  });

  it("ignores a plan_state field that is not a contract value", () => {
    const layout = planLayout(plan, {
      planState: { shape: "spiral", health: "fine" },
    });
    expect(layout.shape).toBe("chain");
    expect(layout.health).toBe("on_track");
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
    // The corpus lists card:initiative twice; the wire preserves duplicates,
    // and an index keyed by ref must still answer for it.
    expect(
      refFixture.refs.filter((ref) => ref === "card:initiative"),
    ).toHaveLength(2);
    expect(refChipModel("card:initiative", resolved, context).resolvable).toBe(
      true,
    );
  });
});
