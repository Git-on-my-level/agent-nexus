// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import PlanView from "../../src/lib/components/PlanView.svelte";
import { refResolveExample } from "../../src/lib/fixtures/refResolveExample.js";
import { indexResolvedRefs } from "../../src/lib/refResolve.js";

const resolved = indexResolvedRefs(refResolveExample);
const NOW = Date.parse("2026-10-04T12:00:00Z");

const step = (id, after = [], extra = {}) => ({
  id,
  title: id,
  after,
  ...extra,
});

/**
 * Core computes shape, status and the critical path; the view only draws them.
 * These helpers state what core would have said so a case can be about layout.
 */
const stateFor = (plan, overrides = {}) => ({
  steps: (plan?.steps ?? []).map((entry) => ({
    id: entry.id,
    status: entry.status ?? "not_started",
    resolvable: Boolean(entry.ref),
  })),
  progress: { done: 0, total: (plan?.steps ?? []).length },
  critical_path: [],
  next_steps: [],
  shape: "chain",
  health: "on_track",
  ...overrides,
});

const mount = (plan, props = {}) =>
  render(PlanView, {
    plan,
    resolved,
    organizationSlug: "scaling",
    workspaceSlug: "anx",
    now: NOW,
    ...props,
  });

/**
 * The tree measures the room it has so the remaining steps fit without
 * scrolling, which means `bind:clientWidth` and so a ResizeObserver. jsdom has
 * none; a stub is enough, because the geometry itself is unit-tested directly.
 */
beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("PlanView", () => {
  it("says so when a card has no plan", () => {
    const { container } = mount(null);
    expect(container.querySelector(".plan-empty").textContent).toContain(
      "No plan yet",
    );
  });

  it("renders a chain as a timeline", () => {
    const plan = {
      steps: [step("spec"), step("build", ["spec"]), step("ship", ["build"])],
    };
    const { container } = mount(plan, { planState: stateFor(plan) });
    expect(container.querySelector("[data-plan-shape]").dataset.planShape).toBe(
      "chain",
    );
    expect(container.querySelector(".plan-track--timeline")).not.toBeNull();
    expect(container.querySelector(".plan-tree")).toBeNull();
  });

  it("renders a branch as a tech tree with edges and a scrollable region", () => {
    const plan = { steps: [step("a"), step("b", ["a"]), step("c", ["a"])] };
    const { container } = mount(plan, {
      planState: stateFor(plan, { shape: "dag" }),
    });
    expect(container.querySelector("[data-plan-shape]").dataset.planShape).toBe(
      "dag",
    );
    expect(container.querySelectorAll(".plan-node")).toHaveLength(3);
    expect(container.querySelectorAll(".plan-edge")).toHaveLength(2);
    const region = container.querySelector(".plan-tree-scroll");
    expect(region.getAttribute("role")).toBe("region");
    expect(region.tabIndex).toBe(0);
  });

  it("highlights the critical path on nodes and edges", () => {
    const plan = { steps: [step("a"), step("b", ["a"]), step("side", ["a"])] };
    const { container } = mount(plan, {
      planState: stateFor(plan, { shape: "dag", critical_path: ["a", "b"] }),
    });
    expect(container.querySelectorAll(".plan-node--critical")).toHaveLength(2);
    expect(container.querySelectorAll(".plan-edge--critical")).toHaveLength(1);
  });

  it("renders separate runs as lanes", () => {
    const plan = {
      steps: [step("a"), step("b", ["a"]), step("x"), step("y", ["x"])],
    };
    const { container } = mount(plan, {
      planState: stateFor(plan, { shape: "lanes" }),
    });
    expect(container.querySelector("[data-plan-shape]").dataset.planShape).toBe(
      "lanes",
    );
    expect(container.querySelectorAll(".plan-lane")).toHaveLength(2);
  });

  it("follows core when one added step turns a timeline into a tree", () => {
    // The shape is core's answer; adding `after:` is what changes core's
    // answer, and the view follows it rather than deciding for itself.
    const chain = { steps: [step("a"), step("b", ["a"]), step("c", ["b"])] };
    const { container, unmount } = mount(chain, {
      planState: stateFor(chain),
    });
    expect(container.querySelector("[data-plan-shape]").dataset.planShape).toBe(
      "chain",
    );
    unmount();

    const branched = { steps: [...chain.steps, step("d", ["a"])] };
    const { container: tree } = mount(branched, {
      planState: stateFor(branched, { shape: "dag" }),
    });
    expect(tree.querySelector("[data-plan-shape]").dataset.planShape).toBe(
      "dag",
    );
  });

  it("falls back to a timeline when core computed no shape", () => {
    const { container } = mount({ steps: [step("a"), step("b", ["a"])] });
    expect(container.querySelector("[data-plan-shape]").dataset.planShape).toBe(
      "chain",
    );
  });

  it("says a status is unknown rather than inventing one", () => {
    const { container } = mount({ steps: [step("a", [], { status: "done" })] });
    // `status` on an authored step is core's input, not its answer.
    expect(container.querySelector(".plan-node__status").textContent).toContain(
      "Status unknown",
    );
  });

  it("marks today between the last past step and the next one due", () => {
    const { container } = mount({
      steps: [
        step("spec", [], { due: "2026-10-01" }),
        step("build", ["spec"], { due: "2026-10-09" }),
      ],
    });
    const items = [...container.querySelectorAll(".plan-track--timeline > li")];
    const marker = items.findIndex((item) =>
      item.classList.contains("plan-today"),
    );
    expect(marker).toBe(1);
    expect(items[marker].getAttribute("aria-label")).toBe("Today");
  });

  it("shows dates where they are given and no marker when none are", () => {
    const dated = mount({ steps: [step("a", [], { due: "2026-11-02" })] });
    expect(dated.container.querySelector(".plan-node__due").textContent).toBe(
      "2026-11-02",
    );
    dated.unmount();

    const undated = mount({ steps: [step("a"), step("b", ["a"])] });
    expect(undated.container.querySelector(".plan-today")).toBeNull();
  });

  it("renders a step's ref as a chip and its status from core's plan state", () => {
    const plan = {
      steps: [
        step("done-step", [], { ref: "card:shared-report-contracts" }),
        step("blocked-step", ["done-step"], { ref: "card:pushed-series" }),
      ],
    };
    const { container } = mount(plan, {
      planState: stateFor(plan, {
        steps: [
          { id: "done-step", status: "done", resolvable: true },
          { id: "blocked-step", status: "blocked", resolvable: true },
        ],
      }),
    });
    const chips = container.querySelectorAll("[data-anx-ref]");
    expect(chips).toHaveLength(2);
    expect(chips[0].textContent).toContain("Shared report contracts");

    const nodes = [...container.querySelectorAll("[data-status]")];
    expect(nodes.some((node) => node.dataset.status === "done")).toBe(true);
    expect(nodes.some((node) => node.dataset.status === "blocked")).toBe(true);
  });

  it("renders an unresolvable step ref as a not-found chip", () => {
    const { container } = mount({
      steps: [step("gone", [], { ref: "card:deleted-thing" })],
    });
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip.classList.contains("anx-ref-chip--missing")).toBe(true);
    expect(chip.textContent).toContain("not found");
  });

  it("falls back to the step title when a step has no ref", () => {
    const { container } = mount({ steps: [step("write-the-spec")] });
    expect(container.querySelector(".plan-node__title").textContent).toBe(
      "write-the-spec",
    );
  });

  it("always offers the step list, which is the diagram's fallback", () => {
    const plan = { steps: [step("a"), step("b", ["a"]), step("c", ["a"])] };
    const { container } = mount(plan, {
      planState: stateFor(plan, { shape: "dag", critical_path: ["a", "b"] }),
    });
    const details = container.querySelector(".plan-steps");
    expect(details.querySelector("summary").textContent).toContain(
      "View plan steps",
    );
    const items = details.querySelectorAll("li");
    expect(items).toHaveLength(3);
    // The dependency an edge draws is also written out in words.
    expect(
      [...items].some((item) => item.textContent.includes("after a")),
    ).toBe(true);
    expect(
      [...items].some((item) =>
        item.textContent.includes("on the critical path"),
      ),
    ).toBe(true);
  });

  it("hides only the edge layer from assistive tech, never the nodes", () => {
    const plan = {
      steps: [
        step("a", [], { ref: "card:initiative-plans" }),
        step("b", ["a"]),
        step("c", ["a"]),
      ],
    };
    const { container } = mount(plan, {
      planState: stateFor(plan, { shape: "dag" }),
    });
    expect(
      container.querySelector(".plan-tree__edges").getAttribute("aria-hidden"),
    ).toBe("true");
    // The chip is still a real focusable link inside the diagram.
    expect(
      container.querySelector(".plan-node a[data-anx-ref]"),
    ).not.toBeNull();
  });

  it("reports a dependency loop and still draws the rest", () => {
    const plan = { steps: [step("ok"), step("a", ["b"]), step("b", ["a"])] };
    const { container } = mount(plan, {
      planState: stateFor(plan, { shape: "dag" }),
    });
    expect(container.querySelector(".plan-issues").textContent).toContain(
      "depend on each other in a loop",
    );
    expect(container.querySelectorAll(".plan-node")).toHaveLength(3);
  });

  it("reports a step pointing at a step that is not in the plan", () => {
    const { container } = mount({ steps: [step("a", ["ghost"])] });
    expect(container.querySelector(".plan-issues").textContent).toContain(
      'depends on "ghost"',
    );
  });
});

describe("PlanView names steps rather than refs", () => {
  const plan = {
    steps: [
      step("spec", [], {
        ref: "card:shared-report-contracts",
        title: "Write the spec",
      }),
      step("ship", ["spec"], { title: "Ship it" }),
    ],
  };

  it("shows the step title, with its ref as a chip beneath it", () => {
    const { container } = mount(plan, {
      planState: stateFor(plan, {
        steps: [
          { id: "spec", status: "active", resolvable: true },
          { id: "ship", status: "not_started", resolvable: false },
        ],
      }),
    });
    const titles = [...container.querySelectorAll(".plan-node__title")].map(
      (node) => node.textContent,
    );
    expect(titles).toContain("Write the spec");
    // The raw ref is not the node's label.
    expect(titles).not.toContain("card:shared-report-contracts");
    // The chip is still there, beneath the title.
    const node = container.querySelector('[data-plan-node="spec"]');
    expect(node.querySelector(".plan-node__title").textContent).toBe(
      "Write the spec",
    );
    expect(node.querySelector("[data-anx-ref]")).not.toBeNull();
  });

  it("folds a finished prefix in the timeline and says how many", () => {
    const long = {
      steps: [
        step("a", [], { title: "A" }),
        step("b", ["a"], { title: "B" }),
        step("c", ["b"], { title: "C" }),
      ],
    };
    const { container } = mount(long, {
      planState: stateFor(long, {
        steps: [
          { id: "a", status: "done", resolvable: false },
          { id: "b", status: "done", resolvable: false },
          { id: "c", status: "active", resolvable: false },
        ],
      }),
    });
    const fold = container.querySelector("[data-plan-done-count]");
    expect(fold.dataset.planDoneCount).toBe("2");
    expect(fold.textContent).toContain("2 steps done");
    // The remaining step is drawn; the folded ones are not.
    expect(container.querySelector('[data-plan-node="c"]')).not.toBeNull();
    expect(container.querySelector('[data-plan-node="a"]')).toBeNull();
    // They are still listed in the fallback, so nothing is actually lost.
    const list = container.querySelector(".plan-steps");
    expect(list.textContent).toContain("A");
    expect(list.textContent).toContain("B");
  });

  it("shows an external step as a chip with its status, never not-found", () => {
    const external = {
      steps: [
        step("pr", [], {
          ref: "https://github.com/Git-on-my-level/agent-nexus/pull/246",
          title: "Land the renderer",
        }),
      ],
    };
    const { container } = mount(external, {
      planState: stateFor(external, {
        steps: [{ id: "pr", status: "active", resolvable: true }],
      }),
    });
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip.textContent).not.toContain("not found");
    expect(chip.getAttribute("href")).toContain("github.com");
  });
});
