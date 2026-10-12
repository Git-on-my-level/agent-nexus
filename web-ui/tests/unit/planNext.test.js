import { describe, expect, it } from "vitest";

import { nextActionOvertaken, planNextSteps } from "../../src/lib/planNext.js";

/**
 * The omi card as core read it: phase 1 done, the consolidation step in
 * flight, and two steps behind it that nothing is waiting on yet.
 */
const plan = {
  steps: [
    { id: "inventory", title: "Phase 1 inventory", after: [] },
    { id: "consolidate", title: "Consolidate the readers", after: [] },
    { id: "approach", title: "Record chosen approach", after: ["inventory"] },
    { id: "rollout", title: "Roll the change out", after: ["approach"] },
  ],
};

const planState = {
  steps: [
    { id: "inventory", status: "done" },
    { id: "consolidate", status: "active" },
    { id: "approach", status: "not_started" },
    { id: "rollout", status: "not_started" },
  ],
  progress: { done: 1, total: 4 },
  critical_path: ["approach", "rollout"],
  next_steps: ["approach", "consolidate"],
};

describe("planNextSteps names what core computed", () => {
  it("names the ids core sent, with their plan titles", () => {
    expect(planNextSteps(plan, planState)).toEqual([
      { id: "approach", title: "Record chosen approach", ref: "" },
      { id: "consolidate", title: "Consolidate the readers", ref: "" },
    ]);
  });

  it("puts core's own next step first, so two surfaces cannot disagree", () => {
    /*
     * `summary.next` is the one step the Tasks row and the Overview tile show.
     * The page shows the whole ready set, but it has to lead with the same
     * step, or the same card reads as waiting on different things in two
     * places.
     */
    expect(
      planNextSteps(plan, planState, { preferred: "consolidate" }).map(
        (step) => step.id,
      ),
    ).toEqual(["consolidate", "approach"]);
  });

  it("prefers the critical path over core's lexicographic order", () => {
    const state = { ...planState, next_steps: ["consolidate", "approach"] };
    expect(planNextSteps(plan, state).map((step) => step.id)).toEqual([
      "approach",
      "consolidate",
    ]);
  });

  it("invents nothing when core computed no ready step", () => {
    expect(planNextSteps(plan, { ...planState, next_steps: [] })).toEqual([]);
    expect(planNextSteps(plan, null)).toEqual([]);
    expect(planNextSteps(null, planState)).toEqual([]);
  });

  it("drops an id that is not in the plan", () => {
    // A plan written since the state was computed, or a step the reader may
    // not see: naming an id with no step would print a raw slug.
    const state = { ...planState, next_steps: ["ghost", "approach"] };
    expect(planNextSteps(plan, state).map((step) => step.id)).toEqual([
      "approach",
    ]);
  });

  it("carries a step's ref so the page can chip it", () => {
    const withRef = {
      steps: [{ id: "pr", title: "Land it", after: [], ref: "card:land" }],
    };
    expect(
      planNextSteps(withRef, { steps: [], next_steps: ["pr"] })[0].ref,
    ).toBe("card:land");
  });
});

describe("nextActionOvertaken reads a hand-written note against the plan", () => {
  it("calls a note that names only a finished step out of date", () => {
    expect(
      nextActionOvertaken("Phase 1 inventory, with Luna", plan, planState),
    ).toBe(true);
  });

  it("keeps a note that names a step still open", () => {
    expect(
      nextActionOvertaken(
        "Finish the Phase 1 inventory, then record chosen approach",
        plan,
        planState,
      ),
    ).toBe(false);
  });

  it("keeps a note that names no step at all", () => {
    // Prose the plan says nothing about is not in conflict with it; the page
    // marks it as hand-written rather than hiding it.
    expect(
      nextActionOvertaken("Ask David which deployment we are keeping", plan, {
        ...planState,
      }),
    ).toBe(false);
  });

  it("does not read a common word as a step reference", () => {
    const short = {
      steps: [
        { id: "ship", title: "Ship", after: [] },
        { id: "later", title: "Later work", after: ["ship"] },
      ],
    };
    const state = {
      steps: [
        { id: "ship", status: "done" },
        { id: "later", status: "not_started" },
      ],
      next_steps: ["later"],
    };
    expect(nextActionOvertaken("Ship the docs rewrite", short, state)).toBe(
      false,
    );
  });

  it("does not read a one-word step id as a step reference", () => {
    // Slugs are ordinary verbs. A note that uses "approach" is not naming
    // the finished step whose id happens to be that word.
    const done = {
      ...planState,
      steps: planState.steps.map((step) =>
        step.id === "approach" ? { ...step, status: "done" } : step,
      ),
      next_steps: ["consolidate"],
    };
    expect(
      nextActionOvertaken(
        "Ask David which approach we are keeping",
        plan,
        done,
      ),
    ).toBe(false);
  });

  it("still recognises a multi-word step id in a note", () => {
    const slugged = {
      steps: [{ id: "phase-1-inventory", title: "Inventory", after: [] }],
    };
    const state = {
      steps: [{ id: "phase-1-inventory", status: "done" }],
      next_steps: [],
    };
    expect(
      nextActionOvertaken("Finish the phase 1 inventory", slugged, state),
    ).toBe(true);
  });

  it("does not let a step named after a status swallow a note about one", () => {
    /*
     * A step titled "In review" is named after a column, and those are the
     * same words any note about state uses. Stripping the grammar first
     * collapsed the title to `review` and hid "Leave this in review until
     * Friday" — a note that is still true, on a card where nothing had moved
     * past it.
     */
    const statusy = {
      steps: [
        { id: "draft", title: "Draft the policy", after: [] },
        { id: "in-review", title: "In review", after: ["draft"] },
      ],
    };
    const state = {
      steps: [
        { id: "draft", status: "done" },
        { id: "in-review", status: "done" },
      ],
      next_steps: [],
    };
    expect(
      nextActionOvertaken("Leave this in review until Friday", statusy, state),
    ).toBe(false);
  });

  it("needs a phrase, not one word the note happens to share", () => {
    const plans = {
      steps: [{ id: "rollout", title: "Roll the change out", after: [] }],
    };
    const state = { steps: [{ id: "rollout", status: "done" }] };
    // "change" is one word of a three-word title, and the note is about a
    // different change entirely.
    expect(
      nextActionOvertaken(
        "Ask David which change we are keeping",
        plans,
        state,
      ),
    ).toBe(false);
    // The title quoted as the author wrote it still counts.
    expect(nextActionOvertaken("Roll the change out", plans, state)).toBe(true);
  });

  it("takes a handle as the exact reference it is", () => {
    const handles = {
      steps: [
        {
          id: "phase-1-inventory",
          title: "Inventory",
          after: [],
          ref: "card:omi-inventory",
        },
      ],
    };
    const state = { steps: [{ id: "phase-1-inventory", status: "done" }] };
    // A one-word title is never enough on its own...
    expect(nextActionOvertaken("Finish the inventory", handles, state)).toBe(
      false,
    );
    // ...but a slug or a ref in a sentence was typed on purpose.
    expect(
      nextActionOvertaken("Waiting on phase-1-inventory", handles, state),
    ).toBe(true);
    expect(
      nextActionOvertaken("Blocked by card:omi-inventory", handles, state),
    ).toBe(true);
    // And a handle inside a longer word is not that handle.
    expect(
      nextActionOvertaken("See phase-1-inventory-v2 instead", handles, state),
    ).toBe(false);
  });

  it("has no opinion on an empty note or a card with no plan", () => {
    expect(nextActionOvertaken("", plan, planState)).toBe(false);
    expect(nextActionOvertaken("Anything", null, null)).toBe(false);
  });
});
