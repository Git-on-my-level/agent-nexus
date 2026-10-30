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
  LANE_COLUMNS,
  laneCells,
  overBudget,
  progressSegments,
  splitDone,
  trayActions,
  trayRows,
} from "../../src/lib/swimlanes.js";
import { removeGroupedStep, sendTopLevel } from "../../src/lib/groupingPlan.js";
import {
  enqueueFailureMessage,
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
      owner: null,
      doneAt: null,
      shortCode: "",
      rollup: null,
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
      earliestPromiseAt: null,
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
      progressSegments({
        byState: { backlog: 1, ready: 1, in_progress: 1, done: 1 },
      }),
    ).toEqual([
      { key: "done", count: 1, share: 0.25 },
      { key: "moving", count: 1, share: 0.25 },
      { key: "not_started", count: 2, share: 0.5 },
    ]);
    expect(progressSegments({ byState: {} })).toEqual([]);
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
            needs_home: true,
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
        needs_home: true,
        is_initiative: false,
        title: "A1 F4",
        phase: "backlog",
        createdAt: "2026-10-24T00:00:00Z",
        createdBy: "",
        promote: null,
        move: "",
        homes: [
          { ref: "card:a1", title: "A1 routing", reasons: ["title_family"] },
        ],
      },
    ]);
  });

  it("reads the creator, the move confirmation and the human workspace hint", () => {
    const [row] = trayRows({
      work: [
        {
          ref: "card:x",
          needs_home: true,
          title: "X",
          created_by: { id: "agent:lane", display: "lane", kind: "agent" },
          next_actions: [
            {
              label: "Choose a home",
              argv: ["anx", "work", "move", "card:x", "--under", "card:a"],
            },
            {
              command_id: "work.move",
              argv: [
                "anx",
                "work",
                "move",
                "card:x",
                "--root",
                "--reason",
                "An independent outcome",
              ],
            },
            {
              command_template: "anx move card card:x --to <workspace-alias>",
              requires_human: true,
              requires_destination: true,
            },
          ],
        },
      ],
    });
    expect(row).toMatchObject({
      createdBy: "lane",
      promote: { ref: "card:x", reason: "An independent outcome" },
      move: "anx move card card:x --to <workspace-alias>",
    });
  });

  it("offers no promote or move it cannot prove", () => {
    expect(
      trayActions([
        // Removed patch actions, malformed moves or another command cannot promote.
        {
          command_id: "work.patch",
          stdin: { if_version: 1, patch: { top_level: true, goal: null } },
        },
        { command_id: "work.patch", stdin: { patch: { top_level: true } } },
        {
          command_id: "work.patch",
          stdin: { if_version: -1, patch: { top_level: true } },
        },
        {
          command_id: "work.fold",
          stdin: { if_version: 1, patch: { top_level: true } },
        },
        {
          command_id: "work.move",
          argv: [
            "anx",
            "work",
            "group",
            "card:x",
            "--root",
            "--reason",
            "Outcome",
          ],
        },
        {
          command_id: "work.move",
          argv: ["anx", "work", "move", "card:x", "--root", "--reason", ""],
        },
        {
          command_id: "work.move",
          argv: [
            "anx",
            "work",
            "move",
            "card:x",
            "--root",
            "--reason",
            "x".repeat(2001),
          ],
        },
        {
          command_id: "work.move",
          argv: [
            "anx",
            "work",
            "move",
            "card:x",
            "--root",
            "--reason",
            "Outcome",
            "--unsorted",
          ],
        },
        // An executable move, or one not marked for a person, is not shown.
        {
          command_template: "anx move card card:x",
          requires_human: true,
          argv: ["anx", "move"],
        },
        { command_template: "anx move card card:x", requires_human: false },
      ]),
    ).toEqual({ promote: null, move: "" });
    expect(trayActions(undefined)).toEqual({ promote: null, move: "" });
  });

  it("splits done children into this week and earlier by done_at", () => {
    const now = Date.parse("2026-10-24T12:00:00Z");
    const done = (ref, doneAt) => ({ ref, state: "done", doneAt });
    const { recent, earlier } = splitDone(
      [
        done("card:today", "2026-10-24T09:00:00Z"),
        done("card:six-days", "2026-10-18T13:00:00Z"),
        done("card:eight-days", "2026-10-16T12:00:00Z"),
        done("card:undated", null),
        done("card:future", "2026-10-25T00:00:00Z"),
      ],
      now,
    );
    expect(recent.map((c) => c.ref)).toEqual(["card:today", "card:six-days"]);
    expect(earlier.map((c) => c.ref)).toEqual([
      "card:eight-days",
      "card:undated",
      "card:future",
    ]);
    expect(LANE_COLUMNS.find((c) => c.key === "done")).toMatchObject({
      label: "Done",
      heading: "Done this week",
    });
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
    // A target of 0 is real: any initiative is over it, none is not.
    expect(budgetLabel({ count: 2, target: 0, truncated: false })).toBe(
      "Initiatives 2/0",
    );
    expect(overBudget({ count: 1, target: 0 })).toBe(true);
    expect(overBudget({ count: 0, target: 0 })).toBe(false);
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
      groupingRequest(
        groupingDraft("ungroup", work, parent, { stepId: "card-1" }),
        "h",
        "k",
      ),
    ).toEqual({
      operation: "work.ungroup",
      request: {
        ref: "card:a1-f4",
        parent: "card:a1",
        stepId: "card-1",
        body: { actor_id: "h", request_key: "k" },
        write: null,
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
      stepId: "card-1",
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

  it("is bound to the step the group added", () => {
    const plan = {
      steps: [
        { id: "authored", ref: "card:a1-f4", after: [] },
        { id: "card-1", ref: "card:a1-f4", after: [] },
      ],
    };
    // Only the group's own step goes; an authored step for the card stays.
    expect(
      removeGroupedStep(plan, "card:a1-f4", "card-1").plan.steps.map(
        (step) => step.id,
      ),
    ).toEqual(["authored"]);
    // Its id now names another card: not ours to remove.
    expect(
      removeGroupedStep(
        { steps: [{ id: "card-1", ref: "card:other", after: [] }] },
        "card:a1-f4",
        "card-1",
      ).changed,
    ).toBe(false);
    // Without the id, two candidates are ambiguous and nothing is removed.
    expect(() => removeGroupedStep(plan, "card:a1-f4")).toThrow(
      /More than one/,
    );
  });
});

describe("review fixes", () => {
  it("counts a needs-you child where its card sits, not by phase", () => {
    const rollup = normalizeBrief({
      initiatives: [
        initiative("card:r", {
          rollup: {
            ...initiative("x").rollup,
            total: 3,
            by_state: { in_progress: 2, done: 1 },
            needing_viewer: 1,
            children: [
              child("card:ask", "in_progress", { needs_viewer: true }),
              child("card:work", "in_progress"),
              child("card:shipped", "done"),
            ],
          },
        }),
      ],
    }).initiatives[0].rollup;
    const counts = Object.fromEntries(
      progressSegments(rollup).map((segment) => [segment.key, segment.count]),
    );
    const cells = laneCells(rollup);
    expect(counts).toEqual({ done: 1, moving: 1, stuck: 1 });
    for (const [key, count] of Object.entries(counts))
      expect(cells[key]).toHaveLength(count);
  });

  it("never makes a series-only loop a lane", () => {
    const rows = normalizeBrief({
      initiatives: [
        initiative("card:root"),
        initiative("card:series-loop", {
          series: [{ ref: "series:signup", name: "signup" }],
        }),
      ],
    }).initiatives;
    expect(buildLanes(rows).map((row) => row.ref)).toEqual(["card:root"]);
  });

  it("says why a grouping change was not saved", () => {
    const storage = Object.assign(new Error("QuotaExceededError"), {
      localPersistence: true,
    });
    expect(enqueueFailureMessage(storage)).toMatch(/on your device/);
    expect(
      enqueueFailureMessage(new Error("Sign in before saving a message.")),
    ).toMatch(/Sign in/);
    expect(
      enqueueFailureMessage(new Error("Choose an initiative to group under.")),
    ).toBe("Choose an initiative to group under. Nothing was sent.");
  });
});

describe("initiative moves and legacy outbox recovery", () => {
  const work = { ref: "card:ramp", title: "Ramp telemetry" };

  it("new promote and undo intents are keyed moves with editable reasons", () => {
    const draft = groupingDraft("promote", work);
    expect(draft.reason).toBe("Made an initiative from Watch");
    draft.reason = "A separate outcome";
    expect(groupingRequest(draft, "human", "k")).toEqual({
      operation: "work.move",
      request: {
        ref: work.ref,
        body: {
          actor_id: "human",
          request_key: "k",
          root: true,
          reason: "A separate outcome",
        },
      },
    });
    const undo = groupingDraft("unpromote", work);
    expect(groupingTitle(undo)).toBe("Put “Ramp telemetry” back in Unsorted");
    expect(groupingRequest(undo, "human", "undo-key").request.body).toEqual({
      actor_id: "human",
      request_key: "undo-key",
      unsorted: true,
      reason: "Returned to Unsorted from Watch",
    });
    draft.reason = "";
    expect(() => groupingRequest(draft, "human", "k")).toThrow(/reason/);
  });

  it.each([true, false])(
    "translates a queued top_level=%s using its original identity",
    async (wanted) => {
      const body = {
        actor_id: "h",
        if_version: 3,
        patch: { top_level: wanted },
      };
      const client = {
        moveWork: vi.fn().mockResolvedValue({ work: { ref: work.ref } }),
        patchWork: vi.fn(),
        getWork: vi.fn(),
      };
      await sendTopLevel(client, { ref: work.ref, body }, "original-key");
      await sendTopLevel(client, { ref: work.ref, body }, "original-key");
      const expected = {
        actor_id: "h",
        request_key: "original-key",
        ...(wanted ? { root: true } : { unsorted: true }),
        reason: wanted
          ? "Made an initiative from Watch"
          : "Returned to Unsorted from Watch",
      };
      expect(client.moveWork.mock.calls).toEqual([
        [work.ref, expected],
        [work.ref, expected],
      ]);
      expect(body).toEqual({
        actor_id: "h",
        if_version: 3,
        patch: { top_level: wanted },
      });
      expect(client.patchWork).not.toHaveBeenCalled();
      expect(client.getWork).not.toHaveBeenCalled();
    },
  );

  it("retains a saved reason and leaves uncertain sends to keyed retry", async () => {
    const offline = Object.assign(new Error("offline"), { status: 503 });
    const client = {
      moveWork: vi
        .fn()
        .mockRejectedValueOnce(offline)
        .mockResolvedValue({ work: { ref: work.ref } }),
    };
    const request = {
      ref: work.ref,
      body: {
        actor_id: "h",
        patch: { top_level: true },
        reason: "My original reason",
      },
    };
    await expect(sendTopLevel(client, request, "saved-key")).rejects.toBe(
      offline,
    );
    await sendTopLevel(client, request, "saved-key");
    expect(client.moveWork.mock.calls[1]).toEqual(
      client.moveWork.mock.calls[0],
    );
    expect(client.moveWork.mock.calls[1][1].reason).toBe("My original reason");
  });

  it("keeps incomplete legacy intents for review instead of inventing a destination or key", async () => {
    const client = { moveWork: vi.fn() };
    await expect(
      sendTopLevel(client, { ref: work.ref, body: { patch: {} } }, "k"),
    ).rejects.toThrow(/incomplete/);
    await expect(
      sendTopLevel(client, {
        ref: work.ref,
        body: { patch: { top_level: true } },
      }),
    ).rejects.toThrow(/incomplete/);
    expect(client.moveWork).not.toHaveBeenCalled();
  });
});
