import { describe, expect, it } from "vitest";

import {
  isComputedClosed,
  planSegments,
  statusChip,
  statusStateOf,
  stepListsModel,
  summaryFromStatus,
  workSummaryModel,
} from "../../src/lib/workSummary.js";

const NOW = Date.parse("2026-10-04T12:00:00Z");
const ago = (ms) => new Date(NOW - ms).toISOString();

/** A card as a core that computes summaries sends it. */
const computed = (overrides = {}) => ({
  ref: "card:release-b",
  title: "Release B",
  summary_text: "Initiative plans, live dashboards and agent ergonomics.",
  phase: "in_progress",
  work_summary: {
    status: {
      state: "blocked",
      label: "Blocked",
      reason: "One step is blocked.",
      since: ago(86_400_000),
    },
    set_status: { state: "in_progress", label: "In progress" },
    progress: { done: 2, total: 5, unit: "steps" },
    next: { id: "ship", title: "Ship it", ref: "card:ship", more: 2 },
    owner: "actor:lead",
    due: "2026-10-09T12:00:00Z",
    age: 7200,
    last_movement_at: ago(2 * 3_600_000),
    attention: { count: 3, oldest_age: 9000 },
    ...overrides,
  },
});

/** The same card from a core that sends the separate legacy fields. */
const legacy = (overrides = {}) => ({
  ref: "card:release-b",
  title: "Release B",
  summary: "Initiative plans, live dashboards and agent ergonomics.",
  phase: "in_progress",
  progress: { done: 2, total: 5 },
  plan_state: {
    steps: [{ id: "spec", status: "done" }],
    progress: { done: 2, total: 5 },
    last_movement_at: ago(2 * 3_600_000),
  },
  updated_at: ago(3 * 3_600_000),
  ...overrides,
});

const planState = (overrides = {}) => ({
  steps: [
    { id: "spec", status: "done", resolvable: true },
    { id: "build-it", status: "active", resolvable: true },
    { id: "ship", status: "not_started", resolvable: false },
  ],
  progress: { done: 1, total: 3 },
  critical_path: ["build-it", "ship"],
  next_steps: ["build-it"],
  shape: "chain",
  health: "on_track",
  last_movement_at: ago(2 * 3_600_000),
  ...overrides,
});

describe("the computed summary", () => {
  const model = (overrides) =>
    workSummaryModel(computed(overrides), { now: NOW });

  it("renders core's own words for the status rather than re-deriving them", () => {
    expect(model().status).toMatchObject({
      state: "blocked",
      label: "Blocked",
      reason: "One step is blocked.",
      tone: "danger",
    });
  });

  it("keeps the stored phase core says disagrees with it", () => {
    expect(model().setStatus).toMatchObject({
      state: "in_progress",
      label: "In progress",
    });
  });

  it("shows no stored phase when core sent none, because they agree", () => {
    expect(model({ set_status: undefined }).setStatus).toBeNull();
  });

  it("renders a state this client has never seen", () => {
    // Core's status vocabulary is open. An unknown state keeps core's label,
    // gets a neutral tone, and sorts with the quiet tail rather than above
    // blocked — a word we do not understand is not evidence of urgency.
    const unknown = model({
      status: { state: "awaiting_vendor", label: "Awaiting vendor" },
    });
    expect(unknown.status).toMatchObject({
      state: "awaiting_vendor",
      label: "Awaiting vendor",
      tone: "neutral",
    });
    expect(unknown.status.rank).toBeGreaterThan(model().status.rank);
  });

  it("carries progress with its unit and both spellings of the count", () => {
    expect(model().progress).toMatchObject({
      done: 2,
      total: 5,
      unit: "steps",
      count: "2/5",
      label: "2/5 steps",
      percent: 40,
    });
  });

  it("counts child cards when that is the unit core chose", () => {
    expect(
      model({ progress: { done: 1, total: 4, unit: "cards", truncated: true } })
        .progress,
    ).toMatchObject({
      unit: "cards",
      // The marker goes on `done`: core's total is exact and only the count
      // of finished children can be a lower bound.
      count: "1+/4",
      label: "1+/4 cards",
      sentence: "at least 1 of 4 cards done",
      truncated: true,
    });
  });

  it("drops a zero-total progress rather than drawing an empty bar", () => {
    expect(
      model({ progress: { done: 0, total: 0, unit: "steps" } }).progress,
    ).toBeNull();
  });

  it("names the next step and counts the rest", () => {
    expect(model().next).toMatchObject({
      id: "ship",
      title: "Ship it",
      ref: "card:ship",
      more: 2,
    });
  });

  it("carries owner, due, age and movement as core sent them", () => {
    expect(model()).toMatchObject({
      owner: "actor:lead",
      due: "2026-10-09T12:00:00Z",
      age: 7200,
      lastMovementAt: ago(2 * 3_600_000),
    });
  });

  it("reports the viewer's open asks, and says when the count is a floor", () => {
    expect(model().attention).toMatchObject({
      count: 3,
      oldestAge: 9000,
      label: "3 asks",
    });
    expect(
      model({ attention: { count: 50, oldest_age: 1, truncated: true } })
        .attention.label,
    ).toBe("50+ asks");
    expect(
      model({ attention: { count: 1, oldest_age: 1 } }).attention.label,
    ).toBe("1 ask");
  });

  it("shows no asks part when nothing is waiting on the viewer", () => {
    expect(model({ attention: undefined }).attention).toBeNull();
    expect(
      model({ attention: { count: 0, oldest_age: 0 } }).attention,
    ).toBeNull();
  });

  it("reads the object when `summary=1` aliased it onto summary", () => {
    const row = computed();
    const aliased = {
      ref: row.ref,
      title: row.title,
      summary: row.work_summary,
      summary_text: row.summary_text,
    };
    expect(workSummaryModel(aliased, { now: NOW }).status.state).toBe(
      "blocked",
    );
  });

  it("reads a resolved ref's preview, which carries the object at summary", () => {
    expect(
      workSummaryModel(
        { ref: "card:a", resolvable: true, summary: computed().work_summary },
        { now: NOW },
      ).status.state,
    ).toBe("blocked");
  });
});

describe("what a planless task reads as", () => {
  /*
   * Pinned deliberately, because it is the common case and it reads oddly.
   *
   * Core computes `no_plan` for any card with no plan steps that is not done,
   * cancelled, blocked, overdue or quiet, and it sends `set_status` whenever
   * the stored phase does not agree with the computed state — which `no_plan`
   * never does. So an ordinary task in progress with no plan reads "No plan ·
   * marked in progress" in the Tasks table, where before this change it read
   * "In progress".
   *
   * The client renders what core computed rather than reordering it: the rule
   * in the brief is that status shows computed health and `set_status` is
   * shown when present. Whether `no_plan` should lead a status column for the
   * majority of tasks is a question about the computation, not the renderer.
   */
  it("leads with no plan and carries the phase as the stored one", () => {
    const model = workSummaryModel(
      {
        ref: "card:plain",
        phase: "in_progress",
        work_summary: {
          status: {
            state: "no_plan",
            label: "No plan",
            reason: "Card has no plan steps.",
          },
          set_status: { state: "in_progress", label: "In progress" },
        },
      },
      { now: NOW },
    );
    expect(model.status.label).toBe("No plan");
    expect(model.setStatus.label).toBe("In progress");
    expect(model.progress).toBeNull();
    expect(model.hasPlan).toBe(false);
  });
});

describe("the legacy fallback", () => {
  const model = (overrides) =>
    workSummaryModel(legacy(overrides), { now: NOW });

  it("reads health through the same vocabulary the computed path uses", () => {
    expect(model({ health: { status: "stalled" } }).status).toMatchObject({
      state: "stale",
      label: "Stale",
      tone: "warn",
    });
    expect(model({ health: { status: "blocked" } }).status.tone).toBe("danger");
    expect(
      model({ plan_health: { state: "at_risk", reason: "two steps slipped" } })
        .status,
    ).toMatchObject({ state: "at_risk", reason: "two steps slipped" });
  });

  it("falls back to the stored phase when core computed no health at all", () => {
    // This is what the Tasks table showed before `work_summary` existed, so a
    // card on an older core keeps a status rather than losing its column.
    const row = { ref: "card:x", title: "X", phase: "review" };
    expect(workSummaryModel(row, { now: NOW }).status).toMatchObject({
      state: "review",
      label: "In review",
    });
  });

  it("reads the board column when that is the only phase a row carries", () => {
    // Board card reads (`/boards/{id}/cards`, and the agent page's recent
    // cards) carry `column_key` rather than `phase`.
    expect(
      workSummaryModel({ ref: "card:x", column_key: "blocked" }, { now: NOW })
        .status,
    ).toMatchObject({ state: "blocked", label: "Blocked", tone: "danger" });
    expect(statusStateOf({ ref: "card:x", column_key: "blocked" })).toBe(
      "blocked",
    );
  });

  it("keeps the source's own status word beside the computed one", () => {
    const model = workSummaryModel(
      {
        ref: "card:x",
        phase: "in_progress",
        source: { authority: "jira", native_status: "In UAT" },
      },
      { now: NOW },
    );
    expect(model.status.label).toBe("In progress");
    expect(model.sourceStatus).toBe("In UAT");
  });

  it("gives a card created here no source status to show", () => {
    // Core sends the computed `source` part only for a non-Nexus authority,
    // so the fallback must not invent one where the computed path has none.
    expect(
      workSummaryModel(
        {
          ref: "card:x",
          phase: "in_progress",
          source: { authority: "nexus", native_status: "in_progress" },
        },
        { now: NOW },
      ).sourceStatus,
    ).toBe("");
  });

  it("prints a source's own words for a phase Nexus has no name for", () => {
    const row = {
      ref: "card:x",
      title: "X",
      source: { authority: "github", native_status: "awaiting triage" },
    };
    expect(workSummaryModel(row, { now: NOW }).status.label).toBe(
      "awaiting triage",
    );
  });

  it("never claims the phase disagrees when core did not compute health", () => {
    const row = { ref: "card:x", title: "X", phase: "in_progress" };
    expect(workSummaryModel(row, { now: NOW }).setStatus).toBeNull();
  });

  it("says the phase disagrees once core has computed a health to compare", () => {
    expect(
      model({ plan_health: { state: "blocked", reason: "" } }).setStatus,
    ).toMatchObject({ state: "in_progress", label: "In progress" });
  });

  it("prefers the plan's progress, which is what the bar is drawn from", () => {
    const row = model({
      progress: { done: 1, total: 6 },
      plan_state: planState(),
    });
    expect(row.progress).toMatchObject({ done: 1, total: 3, count: "1/3" });
    expect(row.segments).toHaveLength(3);
  });

  it("names the next step from the plan state", () => {
    expect(model({ plan_state: planState() }).next).toMatchObject({
      title: "Build it",
      more: 0,
    });
    expect(
      model({
        plan_state: planState({ next_steps: ["build-it", "draft", "review"] }),
      }).next.more,
    ).toBe(2);
  });

  it("prefers plan movement over the card timestamp", () => {
    expect(model({ plan_state: planState() }).lastMovementAt).toBe(
      ago(2 * 3_600_000),
    );
    expect(
      model({ plan_state: null, updated_at: ago(3 * 3_600_000) })
        .lastMovementAt,
    ).toBe(ago(3 * 3_600_000));
  });
});

describe("counting and folding share the renderer's vocabulary", () => {
  /*
   * `statusStateOf` exists so a count over two thousand rows does not build
   * two thousand full models. It has to agree with the model it is a
   * shortcut for, or a chip and the rows it counts would disagree.
   */
  it("gives the same state the full model does", () => {
    for (const input of [
      computed(),
      legacy({ health: { status: "stalled" } }),
      legacy({ plan_health: { state: "at_risk" } }),
      legacy({ plan_state: { health: "blocked" } }),
      { ref: "card:x", phase: "review" },
      { ref: "card:x", column_key: "backlog" },
      { ref: "card:x" },
    ]) {
      expect(statusStateOf(input)).toBe(
        workSummaryModel(input, { now: NOW }).status.state,
      );
    }
  });

  it("calls work closed only when the computation says it is over", () => {
    const marked = (state) => ({
      ref: "card:x",
      phase: "done",
      work_summary: { status: { state, label: state } },
    });
    expect(isComputedClosed(marked("done"))).toBe(true);
    expect(isComputedClosed(marked("cancelled"))).toBe(true);
    // Marked done, computed blocked: not over, however it is filed. A list
    // that folded this away hid the disagreement the status line exists for.
    expect(isComputedClosed(marked("blocked"))).toBe(false);
    expect(isComputedClosed({ ref: "card:x", state: "archived" })).toBe(true);
  });
});

describe("the two paths agree", () => {
  /*
   * The point of the whole change: one card, two cores, one reading. Core's
   * own serialized fixture carries both spellings for the same card, and the
   * states must match.
   */
  it("reports the same state from work_summary and from the legacy fields", () => {
    const row = legacy({ plan_health: { state: "blocked", reason: "r" } });
    const alsoComputed = {
      ...row,
      work_summary: { status: { state: "blocked", label: "Blocked" } },
    };
    expect(workSummaryModel(row, { now: NOW }).status.state).toBe(
      workSummaryModel(alsoComputed, { now: NOW }).status.state,
    );
  });

  it("prefers work_summary when a row carries both", () => {
    const row = {
      ...legacy(),
      plan_health: { state: "on_track", reason: "fine" },
      work_summary: { status: { state: "blocked", label: "Blocked" } },
    };
    expect(workSummaryModel(row, { now: NOW }).status.state).toBe("blocked");
  });
});

describe("freshness", () => {
  it("holds planned work to the three-day initiative cadence", () => {
    /*
     * A plan is a commitment to a sequence, and what a reader wants to know
     * is whether the sequence is moving — which is a slower question than
     * "did anyone touch this card today". The cadence follows the plan rather
     * than the surface, so the Overview card and the Tasks row for the same
     * card colour the same age the same way.
     */
    const planned = workSummaryModel(
      {
        ref: "card:a",
        phase: "in_progress",
        work_summary: {
          status: { state: "on_track", label: "In progress" },
          progress: { done: 1, total: 3, unit: "steps" },
          last_movement_at: ago(2 * 86_400_000),
        },
      },
      { now: NOW },
    );
    expect(planned.hasPlan).toBe(true);
    expect(planned.freshnessKind).toBe("initiative");
    expect(planned.freshness).toMatchObject({ age: "2d", tone: "ok" });
  });

  it("judges unplanned work against the cadence its stored phase implies", () => {
    const summary = (lastMovement, phase) =>
      workSummaryModel(
        {
          ref: "card:a",
          phase,
          work_summary: {
            status: { state: "on_track", label: "In progress" },
            set_status: { state: phase, label: phase },
            last_movement_at: lastMovement,
          },
        },
        { now: NOW },
      );
    expect(summary(ago(2 * 3_600_000), "in_progress").freshness).toMatchObject({
      age: "2h",
      tone: "ok",
    });
    expect(summary(ago(3 * 86_400_000), "in_progress").freshness).toMatchObject(
      { tone: "danger" },
    );
    // A backlog card is expected every fortnight, so three days is fine.
    expect(summary(ago(3 * 86_400_000), "backlog").freshness).toMatchObject({
      tone: "ok",
    });
  });

  it("gives finished work no freshness badge at all", () => {
    /*
     * A freshness badge is a prompt: it says somebody should go and look. A
     * delivered card is not asking for anything, so a red "40d, expected
     * every 3d" on it would be chasing work that is already done.
     */
    for (const state of ["done", "cancelled"]) {
      const model = workSummaryModel(
        {
          ref: "card:a",
          work_summary: {
            status: { state, label: state },
            last_movement_at: ago(40 * 86_400_000),
          },
        },
        { now: NOW },
      );
      expect(model.closed, state).toBe(true);
      expect(model.freshness, state).toBeNull();
    }
  });

  it("gives an archived or trashed card none either", () => {
    for (const lifecycle of ["archived", "trashed"]) {
      expect(
        workSummaryModel(
          {
            ref: "card:a",
            state: lifecycle,
            work_summary: {
              status: { state: "blocked", label: "Blocked" },
              last_movement_at: ago(40 * 86_400_000),
            },
          },
          { now: NOW },
        ).freshness,
      ).toBeNull();
    }
  });

  it("shows none when there is no instant to judge", () => {
    expect(
      workSummaryModel(
        { ref: "card:a", work_summary: { status: { state: "stale" } } },
        { now: NOW },
      ).freshness,
    ).toBeNull();
  });
});

describe("planSegments", () => {
  it("gives one segment per step, flagging the critical path", () => {
    const { segments, overflow } = planSegments(planState());
    expect(segments.map((segment) => segment.status)).toEqual([
      "done",
      "active",
      "not_started",
    ]);
    expect(segments.map((segment) => segment.onCriticalPath)).toEqual([
      false,
      true,
      true,
    ]);
    expect(overflow).toBe(0);
  });

  it("caps a long plan and reports the overflow", () => {
    const steps = Array.from({ length: 30 }, (_, index) => ({
      id: `s${index}`,
      status: "not_started",
    }));
    const { segments, overflow } = planSegments({ steps }, null, 24);
    expect(segments).toHaveLength(24);
    expect(overflow).toBe(6);
  });

  it("gives every segment a unique key even when core repeats an id", () => {
    const { segments } = planSegments({
      steps: [
        { id: "dup", status: "done" },
        { id: "dup", status: "active" },
      ],
    });
    expect(new Set(segments.map((s) => s.key)).size).toBe(2);
  });

  it("returns nothing without a plan state", () => {
    expect(planSegments(null)).toEqual({ segments: [], overflow: 0 });
  });

  it("names the shape, since a card draws a bar rather than the graph", () => {
    const shaped = (shape) =>
      workSummaryModel(legacy({ plan_state: planState({ shape }) }), {
        now: NOW,
      }).shapeLabel;
    expect(shaped("chain")).toBe("Timeline");
    expect(shaped("dag")).toBe("Tech tree");
    expect(shaped("lanes")).toBe("Lanes");
  });
});

describe("stepListsModel", () => {
  const digest = (overrides = {}) => ({
    window_hours: 168,
    completed: {
      items: [
        {
          id: "spec",
          title: "Write the spec",
          ref: "card:spec",
          status: "done",
          at: ago(36 * 3_600_000),
        },
      ],
      more: 0,
    },
    current: {
      items: [{ id: "build-it", title: "Build it", status: "active" }],
      more: 0,
    },
    next: {
      items: [{ id: "ship", title: "Ship it", status: "not_started" }],
      more: 2,
    },
    ...overrides,
  });

  it("names the three lists and ages the completed rows", () => {
    const model = stepListsModel(digest(), { now: NOW });
    expect(model.groups.map((list) => list.key)).toEqual([
      "completed",
      "current",
      "next",
    ]);
    expect(model.groups.map((list) => list.label)).toEqual([
      "Recently completed",
      "Current",
      "Next",
    ]);
    expect(model.windowHours).toBe(168);
    expect(model.completed.items[0]).toMatchObject({
      title: "Write the spec",
      age: "1d",
    });
    // Only a completed row has an instant to show.
    expect(model.current.items[0].age).toBe("");
    expect(model.next.more).toBe(2);
  });

  it("keeps core's omitted count and adds its own cut to it", () => {
    const model = stepListsModel(
      digest({
        current: {
          items: ["a", "b", "c", "d"].map((id) => ({
            id,
            title: id.toUpperCase(),
            status: "active",
          })),
          more: 1,
        },
      }),
      { now: NOW },
    );
    expect(model.current.items.map((step) => step.id)).toEqual(["a", "b", "c"]);
    expect(model.current.more).toBe(2);
  });

  it("lists only the groups that have rows", () => {
    expect(
      stepListsModel(
        digest({
          completed: { items: [], more: 0 },
          current: { items: [], more: 0 },
        }),
        { now: NOW },
      ).groups.map((list) => list.key),
    ).toEqual(["next"]);
  });

  it("returns nothing for a core that computes no digest", () => {
    expect(stepListsModel(undefined)).toBeNull();
    expect(stepListsModel(null)).toBeNull();
  });

  it("gives every row a unique key even when core repeats an id", () => {
    const model = stepListsModel(
      digest({
        current: {
          items: [
            { id: "dup", title: "First", status: "active" },
            { id: "dup", title: "Second", status: "blocked" },
          ],
          more: 0,
        },
      }),
      { now: NOW },
    );
    const keys = model.current.items.map((step) => step.key);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("reaches the model from either spelling of the digest", () => {
    expect(
      workSummaryModel(legacy({ plan_step_digest: digest() }), { now: NOW })
        .steps.groups,
    ).toHaveLength(3);
    expect(
      workSummaryModel(computed({ steps: digest() }), { now: NOW }).steps
        .completed.items[0].title,
    ).toBe("Write the spec");
  });
});

describe("summaryFromStatus", () => {
  it("builds the same model from a state and a reason alone", () => {
    const model = summaryFromStatus(
      {
        state: "at_risk",
        reason: "two steps slipped",
        progress: { done: 1, total: 4 },
      },
      { now: NOW },
    );
    expect(model.status).toMatchObject({
      state: "at_risk",
      label: "At risk",
      tone: "warn",
      reason: "two steps slipped",
    });
    expect(model.progress.count).toBe("1/4");
    expect(model.setStatus).toBeNull();
  });

  it("renders nothing without a state", () => {
    expect(summaryFromStatus({ reason: "something" })).toBeNull();
    expect(summaryFromStatus(null)).toBeNull();
  });
});

describe("statusChip", () => {
  it("labels and tones a tally from the same table a card reads", () => {
    expect(statusChip("no_plan", 3)).toMatchObject({
      state: "no_plan",
      count: 3,
      label: "No plan",
      tone: "neutral",
    });
    expect(statusChip("blocked", 1).tone).toBe("danger");
  });

  it("keeps a state it does not know rather than dropping the count", () => {
    expect(statusChip("awaiting_vendor", 2)).toMatchObject({
      label: "awaiting vendor",
      count: 2,
      tone: "neutral",
    });
  });
});
