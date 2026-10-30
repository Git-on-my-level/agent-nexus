/**
 * A brief shaped like the approved Watch mockups (`Rollup`, `Board`,
 * `BoardPhone`): top-level initiatives with rolled-up children and
 * workstreams (each with its own rollup, short codes and owner), outcomes, a
 * proposed initiative, completion dates, open asks about some of those
 * children, a loop, and an Ungrouped tray with its creator and actions.
 * Generic product names; the shapes and counts are what the mockups show.
 *
 * Used by `tests/e2e/watch-mockup.spec.js` and the `qa-visual` scenes, so
 * the side-by-side screenshots and the baselines read the same data.
 */

const child = (ref, title, state, extra = {}) => ({
  ref,
  title,
  step_id: ref.slice(5),
  state,
  past_promise: false,
  needs_viewer: false,
  ...extra,
});

function rollup(
  children,
  { workstreams = [], total, earliest_promise_at = null } = {},
) {
  const by_state = {};
  for (const row of children)
    by_state[row.state] = (by_state[row.state] || 0) + 1;
  return {
    total: total ?? children.length,
    by_state,
    past_promise: children.filter((row) => row.past_promise).length,
    needing_viewer: children.filter((row) => row.needs_viewer).length,
    earliest_promise_at,
    workstreams: children.filter((row) => workstreams.includes(row.ref)),
    children,
    has_more: false,
  };
}

/**
 * A workstream's own children, as core expands them one level down: one
 * card per `[code, state]`, with the workstream's nearest promise.
 */
function stream(prefix, cards, earliest = null) {
  return rollup(
    cards.map(([code, state, extra], i) =>
      child(`card:${prefix}-${i + 1}`, `${prefix} ${i + 1}`, state, {
        short_code: code,
        ...extra,
      }),
    ),
    { earliest_promise_at: earliest },
  );
}

function initiative(ref, title, children, extra = {}) {
  const { workstreams, total, ...rest } = extra;
  return {
    ref,
    title,
    owner: null,
    state: "moving",
    placement: "in_progress",
    promise: null,
    past_promise_for_seconds: 0,
    promise_moves: 0,
    last_evidence_at: null,
    progress: null,
    blocked_count: 0,
    waiting_on_you_count: 0,
    note: null,
    possibly_moot: { flagged: false },
    series: [],
    metric_flat: null,
    loop: null,
    parent_ref: null,
    rollup: rollup(children, { workstreams, total }),
    ...rest,
  };
}

const ask = (id, title, subject, subjectTitle, now) => ({
  ref: `inbox:${id}`,
  kind: "ask",
  inbox_item_id: id,
  event_ref: `event:${id}`,
  title,
  body: "",
  requested_by: { id: "agent:lead", display: "lead agent" },
  created_at: new Date(Date.parse(now) - 3_600_000).toISOString(),
  decide_by: null,
  overdue_for_seconds: 0,
  blocks: [],
  blocks_count: 0,
  priority: "high",
  subject: { ref: subject, title: subjectTitle },
  proposals: ["Yes", "No"],
  age_seconds: 3600,
});

export function mockupBrief(now = "2026-10-24T12:00:00Z") {
  const at = Date.parse(now);
  const iso = (hours) => new Date(at + hours * 3_600_000).toISOString();
  const capture = [
    child("card:on-device-stt", "On-device STT for Japanese", "backlog"),
    child("card:data-loss-audit", "Data-loss audit", "ready"),
    child(
      "card:record-lane",
      "Record lane & legacy retirement",
      "in_progress",
      {
        owner: "actor-lead",
        rollup: stream(
          "record",
          [
            ["t1", "done"],
            ["t2", "in_progress"],
            ["p86", "in_progress"],
            ["p90", "ready"],
            ["", "in_progress"],
            ["", "in_progress"],
            ["", "ready"],
            ["", "backlog"],
            ["", "review"],
          ],
          iso(26),
        ),
      },
    ),
    child("card:speaker-id", "Speaker identification", "in_progress", {
      past_promise: true,
      owner: "actor-lead",
      rollup: stream(
        "speaker",
        [
          ["", "ready", { past_promise: true }],
          ["", "in_progress"],
          ["", "in_progress"],
          ["", "review"],
        ],
        iso(-10),
      ),
    }),
    child("card:custody", "Custody & merging", "review", {
      owner: "actor-agents",
      rollup: stream("custody", [
        ["wal", "in_progress"],
        ["wal", "in_progress"],
        ["p87", "review"],
        ["", "in_progress"],
        ["", "ready"],
        ["", "done"],
        ["", "done"],
        ["", "backlog"],
      ]),
    }),
    child(
      "card:fallback-funding",
      "Fallback funding & autopay ceiling",
      "blocked",
      {
        needs_viewer: true,
      },
    ),
    child("card:held-capture", "Held-capture UX, 72h retention", "backlog", {
      needs_viewer: true,
    }),
    child("card:speaker-collision", "Unknown-speaker collision", "blocked", {
      past_promise: true,
    }),
    child("card:fail-loudly", "Fail loudly", "done", {
      done_at: iso(-30),
      short_code: "obs",
    }),
  ];
  const margin = [
    child("card:storage-levers", "Storage levers", "backlog"),
    child("card:db-reads", "Database reads within budget", "in_progress", {
      owner: "actor-agents",
      rollup: stream(
        "reads",
        [
          ["db-p1", "blocked", { past_promise: true }],
          ["db-p2", "blocked", { past_promise: true }],
          ["db-p3", "blocked", { past_promise: true }],
          ["db-p4", "in_progress"],
          ["db-p5", "in_progress"],
          ["", "in_progress"],
          ["", "in_progress"],
          ["", "done"],
        ],
        iso(-30),
      ),
    }),
    child("card:gpu-sizing", "GPU & compute sizing", "in_progress", {
      owner: "actor-lead",
      rollup: stream(
        "gpu",
        [
          ["cost", "in_progress"],
          ["cost", "in_progress"],
          ["", "ready"],
          ["", "in_progress"],
          ["", "backlog"],
        ],
        iso(21 * 24),
      ),
    }),
    child("card:pricing-ladder", "Pricing ladder & billing", "review", {
      rollup: stream("pricing", [
        ["", "review", { needs_viewer: true }],
        ["", "in_progress"],
        ["", "ready"],
      ]),
    }),
    child("card:pro-price", "Pro price: tier A or tier B?", "blocked", {
      needs_viewer: true,
    }),
    child("card:cache-bucket", "Regional cache bucket + IAM", "blocked", {
      needs_viewer: true,
      past_promise: true,
    }),
    child("card:usage-export", "Usage export", "done", {
      done_at: iso(-10 * 24),
    }),
  ];
  const retention = [
    child("card:device-v1", "Device v1", "backlog"),
    child("card:mobile-quality", "Mobile quality & release", "in_progress"),
    child("card:desktop-v5", "Desktop v5", "in_progress"),
    child("card:retention-target", "Set a retention target", "ready"),
  ];
  const smarter = [
    child("card:weekly-recall", "Weekly recall", "backlog"),
    child("card:background-agent", "Background agent", "in_progress"),
    child("card:proactivity", "Proactivity v2 experiments", "in_progress"),
    child("card:task-extraction", "Task extraction quality", "review"),
  ];
  const security = [
    child("card:keyless", "Keyless program", "in_progress"),
    child("card:constant-time", "Admin secret constant-time", "in_progress"),
    child("card:plugins", "Keep or retire third-party plugins", "blocked", {
      needs_viewer: true,
    }),
    child("card:hosting-exit", "Hosting account exit", "blocked", {
      past_promise: true,
    }),
  ];
  const brief = {
    schema: "brief.v1",
    generated_at: now,
    principal: { id: "actor-reader", display: "Reader", kind: "human" },
    curation_suggestions: [],
    decisions: [
      ask(
        "ask-funding",
        "Fallback funding & autopay ceiling",
        "card:fallback-funding",
        "Fallback funding & autopay ceiling",
        now,
      ),
      ask(
        "ask-held",
        "Held-capture UX: keep 72h?",
        "card:held-capture",
        "Held-capture UX, 72h retention",
        now,
      ),
      ask(
        "ask-price",
        "Pro price: tier A or tier B?",
        "card:pro-price",
        "Pro price: tier A or tier B?",
        now,
      ),
      ask(
        "ask-cache",
        "Regional cache bucket + IAM",
        "card:cache-bucket",
        "Regional cache bucket + IAM",
        now,
      ),
      ask(
        "ask-plugins",
        "Keep or retire third-party plugins?",
        "card:plugins",
        "Keep or retire third-party plugins",
        now,
      ),
    ],
    initiatives: [
      initiative(
        "card:capture",
        "Every word captured, transcribed & attributed",
        capture,
        {
          workstreams: ["card:record-lane", "card:speaker-id", "card:custody"],
          owner: "actor-lead",
          outcome:
            "every recording synced on the record lane · legacy queues gone · no silent loss · right speaker",
          state: "past_promise",
          past_promise_for_seconds: 36_000,
          promise: {
            at: iso(-10),
            what: "legacy queues gone, no silent loss",
            source: "note",
          },
          last_evidence_at: iso(-3),
        },
      ),
      initiative(
        "card:margin",
        "Unit economics: margin 50% & revenue per user",
        margin,
        {
          workstreams: [
            "card:db-reads",
            "card:gpu-sizing",
            "card:pricing-ladder",
          ],
          owner: "actor-lead",
          outcome:
            "cost within budget against monthly revenue · revenue per user up",
          waiting_on_you_count: 1,
          promise: {
            at: iso(72),
            what: "cost within budget against monthly revenue",
            source: "note",
          },
          last_evidence_at: iso(-6),
        },
      ),
      initiative("card:retention", "A product users keep using", retention, {
        outcome: "established-user retention up · target not set yet",
        state: "past_promise",
        past_promise_for_seconds: 86_400,
        promise: {
          at: iso(-24),
          what: "established-user retention up",
          source: "note",
        },
        last_evidence_at: iso(-20),
      }),
      initiative("card:smarter", "The assistant gets smarter", smarter, {
        owner: "actor-agents",
        outcome: "proactive help users act on · readouts per experiment",
        // Still in Backlog: core marks it proposed.
        placement: "backlog",
        proposed: true,
        promise: {
          at: iso(48),
          what: "readouts per experiment",
          source: "note",
        },
        last_evidence_at: iso(-2),
      }),
      initiative("card:security", "Security & trust", security, {
        owner: "actor-lead",
        outcome:
          "no long-lived keys in production · hosting exposure contained",
        promise: {
          at: iso(96),
          what: "no long-lived keys in production",
          source: "note",
        },
        last_evidence_at: iso(-30),
      }),
      initiative(
        "card:firmware",
        "Device firmware",
        [child("card:fw-1", "Bootloader update", "in_progress")],
        {
          state: "no_promise",
          placement: "ready",
          last_evidence_at: iso(-72),
        },
      ),
      initiative("card:docs-site", "Docs site refresh", [], {
        state: "done",
        placement: "done",
        last_evidence_at: iso(-48),
      }),
    ],
    loops: [
      initiative("card:weekly-digest", "Weekly digest loop", [], {
        loop: {
          health: "on cadence",
          state: "healthy",
          runs: [],
          cadence: { every_seconds: 604800 },
        },
      }),
    ],
    your_asks: [],
    answered_for_you: [],
    truncated: { initiatives: false, loops: false, decisions: false },
    initiative_budget: { count: 7, target: 15, truncated: false },
    next_actions: [],
  };
  return brief;
}

/** Core's tray actions: the fenced promote and the human-only move hint. */
const trayActions = (ref) => [
  {
    label: "Make it an initiative",
    command_id: "work.move",
    argv: [
      "anx",
      "work",
      "move",
      ref,
      "--root",
      "--reason",
      "An independent outcome",
    ],
    mutates: true,
    side_effect_class: "remote_coordination_write",
  },
  {
    label: "Move to another workspace",
    command_template: `anx move card ${ref} --to <workspace-alias>`,
    requires_human: true,
    requires_destination: true,
    mutates: true,
    side_effect_class: "remote_coordination_write",
  },
];

export function mockupUngrouped(now = "2026-10-24T12:00:00Z") {
  const ago = (minutes) =>
    new Date(Date.parse(now) - minutes * 60_000).toISOString();
  return [
    {
      ref: "card:retry-budget",
      title: "Retry budget for funded failover",
      phase: "backlog",
      parent_ref: null,
      created_at: ago(2),
      created_by: { id: "agent:lane-stt", display: "lane-stt", kind: "agent" },
      likely_home: [
        {
          ref: "card:capture",
          title: "Every word captured, transcribed & attributed",
          reasons: ["title_family"],
        },
      ],
      next_actions: trayActions("card:retry-budget", 2),
    },
    {
      ref: "card:egress",
      title: "Cost 4/5: hosting egress",
      phase: "backlog",
      parent_ref: null,
      created_at: ago(60),
      created_by: null,
      likely_home: [
        {
          ref: "card:margin",
          title: "Unit economics: margin 50% & revenue per user",
          reasons: ["title_family", "same_repository"],
        },
      ],
      next_actions: trayActions("card:egress", 1),
    },
    {
      ref: "card:fleet-capacity",
      title: "Fleet capacity: agent oversubscription",
      phase: "backlog",
      parent_ref: null,
      created_at: ago(26 * 60),
      created_by: { id: "actor-reader", display: "Reader", kind: "human" },
      likely_home: [],
      next_actions: trayActions("card:fleet-capacity", 4),
    },
    {
      ref: "card:ramp-telemetry",
      title: "Shared-speech ramp telemetry",
      phase: "backlog",
      parent_ref: null,
      created_at: ago(180),
      created_by: null,
      likely_home: [],
      next_actions: trayActions("card:ramp-telemetry", 3),
    },
  ];
}

/** The ask core recommends when the set is over target. */
export const OVER_TARGET_ASK =
  "Initiatives exceed the target: fold this initiative or keep it?";

/**
 * The mockup brief with the initiative set over a target of 5, an agent's
 * open ask about it, and recent membership changes of each kind.
 */
export function initiativeSetBrief(now = "2026-10-24T12:00:00Z") {
  const brief = mockupBrief(now);
  const at = Date.parse(now);
  const iso = (hours) => new Date(at + hours * 3_600_000).toISOString();
  const membership = (initiative, board) => ({ board_ref: board, initiative });
  const change = (n, type, ref, title, reason, hours, into) => ({
    ref: `event:membership-${n}`,
    ts: iso(hours),
    type,
    actor_id: "actor-lead",
    payload: {
      subject_ref: ref,
      title,
      reason,
      before: membership(!into, into ? "board:tasks" : "board:initiatives"),
      after: membership(into, into ? "board:initiatives" : "board:tasks"),
    },
  });
  brief.initiative_budget = { count: 7, target: 5, truncated: false };
  brief.over_target = {
    count: 7,
    target: 5,
    truncated: false,
    next_actions: [
      {
        command_id: "ask",
        argv: ["anx", "ask", OVER_TARGET_ASK, "--subject-ref", "card:capture"],
        mutates: true,
        side_effect_class: "remote_coordination_write",
      },
    ],
  };
  brief.decisions = [
    ...brief.decisions,
    ask("ask-count", OVER_TARGET_ASK, "card:capture", "Capture", now),
  ];
  brief.initiative_changes = {
    items: [
      change(
        1,
        "initiative_promoted",
        "card:security",
        "Security & trust",
        "Separate outcome with its own owner",
        -2,
        true,
      ),
      change(
        2,
        "initiative_retired",
        "card:legacy-onboarding",
        "Legacy onboarding flow",
        "",
        -26,
        false,
      ),
      change(
        3,
        "initiative_moved",
        "card:margin",
        "Unit economics: margin 50% & revenue per user",
        "Board cleanup",
        -50,
        true,
      ),
    ],
    truncated: false,
  };
  return brief;
}
