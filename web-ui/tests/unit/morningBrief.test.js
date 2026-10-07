import { describe, expect, it } from "vitest";

import {
  BRIEF_SECTIONS,
  briefClock,
  briefHealth,
  morningBriefModel,
} from "../../src/lib/morningBrief.js";

const brief = (overrides = {}) => ({
  status: "ok",
  generated_at: "2026-10-08T08:12:00Z",
  section_limit: 5,
  throughput_hours: 24,
  decisions: {
    status: "ok",
    count: 26,
    more: 21,
    href: "/inbox?mailbox=needs-you",
    items: [
      {
        id: "task:card:gate",
        title: "Approve the pricing change",
        href: "/tasks/gate",
        reason: "blocks 2 cards · 3d without a change",
        source: "Pick a price",
        signals: { kind: "task", blocks: 2, age_hours: 74 },
      },
    ],
  },
  since_last_look: {
    since: "2026-10-07T20:12:00Z",
    total: 5,
    groups: [
      {
        key: "completed",
        label: "Finished",
        count: 4,
        more: 2,
        items: [
          { ref: "card:a", title: "Shipped the proxy", href: "/tasks/a" },
          { ref: "card:b", title: "Shipped the badge", href: "/tasks/b" },
        ],
      },
      {
        key: "newly_blocked",
        label: "Newly blocked",
        count: 1,
        more: 0,
        items: [
          { ref: "card:c", title: "Lost the credential", href: "/tasks/c" },
        ],
      },
    ],
  },
  at_risk: {
    status: "ok",
    count: 1,
    more: 0,
    items: [
      {
        ref: "card:billing",
        title: "Billing",
        href: "/tasks/billing",
        state: "blocked",
        reason: "An unfinished step or dependency is blocked.",
        since: "2026-10-06T06:12:00Z",
        progress: { done: 1, total: 4 },
      },
    ],
  },
  machine: {
    status: "ok",
    roster_status: "ok",
    working: 2,
    waiting: 1,
    stuck: 1,
    finished_24h: 7,
    agents_finished_24h: 3,
    href: "/agents",
    stuck_items: [
      {
        ref: "actor:holder",
        title: "Holder",
        href: "/agents/holder",
        reason: "Silent 5h: no signal while holding Migrate the index.",
      },
    ],
  },
  initiatives: {
    status: "ok",
    count: 7,
    more: 2,
    href: "/tasks",
    by_state: { no_plan: 5, blocked: 1, on_track: 1 },
    items: [
      {
        ref: "card:i2",
        title: "Billing",
        href: "/tasks/i2",
        state: "blocked",
        reason: "An unfinished step or dependency is blocked.",
        progress: { done: 1, total: 4 },
      },
      {
        ref: "card:i1",
        title: "Hosted onboarding",
        href: "/tasks/i1",
        state: "no_plan",
        reason: "Initiative has no plan steps.",
        progress: { done: 0, total: 3 },
      },
    ],
  },
  ...overrides,
});

const scoped = (path) => `/o/scaling/w/anx${path}`;

describe("morningBriefModel", () => {
  it("is null on a core that sends no brief", () => {
    // Distinct from a brief with nothing in it: an older core is not a quiet
    // morning, and the band must not claim one.
    expect(morningBriefModel(undefined)).toBeNull();
    expect(morningBriefModel(null)).toBeNull();
    expect(morningBriefModel(brief())).not.toBeNull();
  });

  it("keeps the five questions in order and binds every row to this workspace", () => {
    const model = morningBriefModel(brief(), { hrefFor: scoped });
    expect(model.order).toEqual([...BRIEF_SECTIONS]);
    expect(model.sections.decisions.rows[0].href).toBe(scoped("/tasks/gate"));
    expect(model.sections.decisions.moreHref).toBe(
      scoped("/inbox?mailbox=needs-you"),
    );
    expect(model.sections.machine.rows[0].href).toBe(scoped("/agents/holder"));
    expect(model.sections.initiatives.moreHref).toBe(scoped("/tasks"));
  });

  it("carries the ranking reason through untouched", () => {
    // The reason is core's: the client must not re-derive or re-word it, or
    // the ranking and its explanation can disagree.
    const model = morningBriefModel(brief());
    const top = model.sections.decisions.rows[0];
    expect(top.reason).toBe("blocks 2 cards · 3d without a change");
    expect(top.blocks).toBe(2);
    expect(model.sections.decisions.total).toBe(26);
    expect(model.sections.decisions.more).toBe(21);
  });

  it("says what an empty section means rather than leaving a blank", () => {
    const model = morningBriefModel(
      brief({
        decisions: { status: "ok", count: 0, more: 0, items: [] },
        at_risk: { status: "ok", count: 0, more: 0, items: [] },
        since_last_look: {
          since: "2026-10-08T07:12:00Z",
          total: 0,
          groups: [],
        },
      }),
    );
    expect(model.sections.decisions.emptyLine).toBe(
      "Nothing is waiting on your decision.",
    );
    expect(model.sections.risk.emptyLine).toBe("Nothing is off track.");
    expect(model.sections.changes.emptyLine).toMatch(
      new RegExp(
        `^Nothing new since ${briefClock("2026-10-08T07:12:00Z")}\\.$`,
      ),
    );
  });

  it("tells a first visit apart from a quiet one", () => {
    const model = morningBriefModel(
      brief({
        since_last_look: {
          since: null,
          first_visit: true,
          total: 0,
          groups: [],
        },
      }),
    );
    expect(model.sections.changes.firstVisit).toBe(true);
    expect(model.sections.changes.emptyLine).toContain("First look");
    expect(model.sections.changes.emptyLine).not.toContain("Nothing new");
  });

  it("groups the digest with counts before items", () => {
    const model = morningBriefModel(brief(), { hrefFor: scoped });
    const [first, second] = model.sections.changes.groups;
    expect(model.sections.changes.total).toBe(5);
    expect(first).toMatchObject({
      key: "completed",
      label: "Finished",
      count: 4,
      more: 2,
    });
    expect(first.rows).toHaveLength(2);
    expect(first.rows[0].href).toBe(scoped("/tasks/a"));
    expect(second.key).toBe("newly_blocked");
  });

  it("reports a planless initiative as no plan, never as healthy", () => {
    const model = morningBriefModel(brief());
    const planless = model.sections.initiatives.rows.find(
      (row) => row.ref === "card:i1",
    );
    expect(planless.health.state).toBe("no_plan");
    expect(planless.health.short).toBe("No plan");
    expect(planless.health.tone).toBe("neutral");
    expect(planless.progress).toEqual({ done: 0, total: 3, percent: 0 });

    // The state counts are the point: five initiatives with no plan is a
    // sentence, where five green chips were a lie.
    expect(model.sections.initiatives.chips).toEqual([
      { state: "blocked", count: 1, rank: 0, label: "Blocked", tone: "danger" },
      { state: "on_track", count: 1, rank: 3, label: "On track", tone: "ok" },
      {
        state: "no_plan",
        count: 5,
        rank: 5,
        label: "No plan",
        tone: "neutral",
      },
    ]);
  });

  it("keeps a risk row's computed reason and its progress", () => {
    const model = morningBriefModel(brief());
    const row = model.sections.risk.rows[0];
    expect(row.health.state).toBe("blocked");
    expect(row.reason).toBe("An unfinished step or dependency is blocked.");
    expect(row.health.reason).toBe(row.reason);
    expect(row.progress).toEqual({ done: 1, total: 4, percent: 25 });
    expect(row.since).toBe("2026-10-06T06:12:00Z");
  });

  it("colours only stuck and waiting, because offline is the normal state", () => {
    const model = morningBriefModel(brief());
    const tones = Object.fromEntries(
      model.sections.machine.stats.map((stat) => [stat.key, stat.tone ?? ""]),
    );
    expect(tones).toEqual({
      working: "",
      waiting: "warn",
      finished: "",
      stuck: "warn",
    });
    expect(
      model.sections.machine.stats.find((stat) => stat.key === "finished")
        .label,
    ).toBe("Finished in 24h");
    expect(model.sections.machine.rows[0].reason).toContain(
      "Migrate the index",
    );
  });

  it("keeps throughput when the roster read fails", () => {
    const model = morningBriefModel(
      brief({
        machine: {
          status: "ok",
          roster_status: "unavailable",
          message: "Agent presence could not be loaded.",
          working: null,
          waiting: null,
          stuck: null,
          finished_24h: 7,
          agents_finished_24h: 3,
          href: "/agents",
          stuck_items: [],
        },
      }),
    );
    const machine = model.sections.machine;
    expect(machine.rosterDown).toBe(true);
    expect(machine.stats.map((stat) => stat.key)).toEqual(["finished"]);
    expect(machine.stats[0].value).toBe(7);
    expect(machine.message).toBe("Agent presence could not be loaded.");
  });

  it("reports a failed risk read instead of nothing off track", () => {
    const model = morningBriefModel(
      brief({
        at_risk: {
          status: "unavailable",
          message: "Risk could not be computed for this reader.",
          count: 0,
          items: [],
        },
      }),
    );
    expect(model.sections.risk.status).toBe("unavailable");
    expect(model.sections.risk.message).toBe(
      "Risk could not be computed for this reader.",
    );
    expect(model.sections.risk.empty).toBeUndefined();
    expect(model.sections.risk.rows).toEqual([]);
  });

  it("omits finished rather than reporting zero when throughput is unavailable", () => {
    const model = morningBriefModel(
      brief({
        machine: {
          status: "ok",
          roster_status: "ok",
          throughput_status: "unavailable",
          working: 2,
          waiting: 0,
          stuck: 0,
          finished_24h: null,
          agents_finished_24h: null,
          href: "/agents",
          stuck_items: [],
        },
      }),
    );
    const machine = model.sections.machine;
    expect(machine.throughputDown).toBe(true);
    expect(machine.stats.map((stat) => stat.key)).toEqual([
      "working",
      "waiting",
      "stuck",
    ]);
    expect(machine.message).toBe("Throughput could not be computed.");
  });

  it("reports a failed decisions read instead of an empty ranking", () => {
    const model = morningBriefModel(
      brief({
        decisions: {
          status: "unavailable",
          message: "Decisions could not be ranked.",
          count: 0,
          items: [],
        },
      }),
    );
    expect(model.sections.decisions.status).toBe("unavailable");
    expect(model.sections.decisions.message).toBe(
      "Decisions could not be ranked.",
    );
    expect(model.sections.decisions.rows).toEqual([]);
  });

  it("survives a brief whose sections are missing", () => {
    // A field a newer or older core does not send must not take the band down.
    const model = morningBriefModel({ status: "ok" });
    expect(model.order).toEqual([...BRIEF_SECTIONS]);
    for (const key of BRIEF_SECTIONS) {
      expect(model.sections[key].rows).toEqual([]);
    }
    expect(model.sections.initiatives.chips).toEqual([]);
    expect(model.sections.machine.stats.map((stat) => stat.value)).toEqual([
      0, 0, 0, 0,
    ]);
  });
});

describe("briefHealth", () => {
  it("uses the shared health vocabulary and never invents one", () => {
    expect(
      briefHealth("no_plan", "Initiative has no plan steps."),
    ).toMatchObject({
      state: "no_plan",
      short: "No plan",
      known: true,
      rank: 5,
    });
    expect(briefHealth("at_risk").rank).toBe(1);
    expect(briefHealth("")).toBeNull();
    expect(briefHealth("invented")).toBeNull();
  });
});

describe("digest row identity", () => {
  /*
   * Two completed steps of one initiative share the initiative's ref. Keyed
   * rendering on ref alone threw `each_key_duplicate` in both the dev and the
   * production build, so expanding "Plan steps done" took the page down.
   */
  const twoSteps = (overrides = {}) =>
    brief({
      since_last_look: {
        since: "2026-10-07T20:12:00Z",
        total: 2,
        groups: [
          {
            key: "steps",
            label: "Plan steps done",
            count: 2,
            more: 0,
            items: [
              {
                ref: "card:initiative",
                title: "Draft the brief",
                href: "/tasks/initiative",
                step_id: "draft",
              },
              {
                ref: "card:initiative",
                title: "Review the brief",
                href: "/tasks/initiative",
                step_id: "review",
              },
            ],
          },
        ],
        ...overrides,
      },
    });

  it("gives two steps of one initiative distinct keys", () => {
    const model = morningBriefModel(twoSteps());
    const [group] = model.sections.changes.groups;
    const keys = group.rows.map((row) => row.key);
    expect(group.rows).toHaveLength(2);
    expect(new Set(keys).size).toBe(2);
    expect(group.rows.map((row) => row.stepId)).toEqual(["draft", "review"]);
  });

  it("keeps keys unique even when the rows are indistinguishable", () => {
    // A core that sends no step_id, or two genuinely identical rows, must
    // still render: a duplicate key is a crash, not a cosmetic problem.
    const model = morningBriefModel(
      brief({
        since_last_look: {
          since: "2026-10-07T20:12:00Z",
          total: 2,
          groups: [
            {
              key: "steps",
              label: "Plan steps done",
              count: 2,
              more: 0,
              items: [
                { ref: "card:initiative", title: "A step", href: "/tasks/x" },
                { ref: "card:initiative", title: "A step", href: "/tasks/x" },
              ],
            },
          ],
        },
      }),
    );
    const keys = model.sections.changes.groups[0].rows.map((row) => row.key);
    expect(new Set(keys).size).toBe(2);
  });

  it("does not collide across groups that share a ref", () => {
    const model = morningBriefModel(
      brief({
        since_last_look: {
          since: "2026-10-07T20:12:00Z",
          total: 2,
          groups: [
            {
              key: "completed",
              label: "Finished",
              count: 1,
              more: 0,
              items: [{ ref: "card:x", title: "Done", href: "/tasks/x" }],
            },
            {
              key: "updated",
              label: "Updated",
              count: 1,
              more: 0,
              items: [{ ref: "card:x", title: "Done", href: "/tasks/x" }],
            },
          ],
        },
      }),
    );
    const keys = model.sections.changes.groups.flatMap((group) =>
      group.rows.map((row) => row.key),
    );
    expect(new Set(keys).size).toBe(keys.length);
  });
});
