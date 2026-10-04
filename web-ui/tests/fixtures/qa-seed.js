const FIXED_NOW_ISO = "2026-03-12T14:00:00.000Z";

export const QA_FIXED_NOW_ISO = FIXED_NOW_ISO;
export const QA_FIXED_NOW_MS = Date.parse(FIXED_NOW_ISO);
export const QA_HOME_HANDOFF_PARTIAL_MARK_ISO = hoursAgo(12);
export const QA_HOME_HANDOFF_ZERO_MARK_ISO = QA_FIXED_NOW_ISO;

function atOffsetMs(offsetMs) {
  return new Date(QA_FIXED_NOW_MS + offsetMs).toISOString();
}

function hoursAgo(hours) {
  return atOffsetMs(-hours * 60 * 60 * 1000);
}

function daysAgo(days) {
  return atOffsetMs(-days * 24 * 60 * 60 * 1000);
}

export const QA_ACTORS = [
  {
    id: "actor-jordan-human",
    display_name: "Jordan (Human operator)",
    tags: ["human", "operator"],
    created_at: daysAgo(90),
  },
  {
    id: "actor-zara-ops",
    display_name: "Zara (Ops AI)",
    tags: ["ops", "coordinator"],
    created_at: daysAgo(90),
  },
  {
    id: "actor-soren-release",
    display_name: "Soren (Release captain)",
    tags: ["release", "qa"],
    created_at: daysAgo(60),
  },
  {
    id: "actor-iris-docs",
    display_name: "Iris (Docs lead)",
    tags: ["docs", "editor"],
    created_at: daysAgo(45),
  },
];

export const QA_AUTH_AGENT = {
  agent_id: "principal_jordan_human",
  actor_id: "actor-jordan-human",
  username: "jordan@agentnexus.dev",
  principal_kind: "human",
  auth_method: "passkey",
};

export const QA_PRINCIPALS = [
  { ...QA_AUTH_AGENT, auth_admin: false },
  {
    agent_id: "principal_zara_agent",
    actor_id: "actor-zara-ops",
    username: "codex.qa-mbp",
    principal_kind: "agent",
    auth_method: "host_assertion",
    auth_admin: true,
  },
];

export const QA_AUTH_ADMINS = [
  {
    principal_id: "principal_zara_agent",
    actor_id: "actor-zara-ops",
    username: "codex.qa-mbp",
    auth_admin: true,
    host_id: "host_qa_mbp",
    host_slug: "qa-mbp",
    agent_name: "codex",
  },
];

export const QA_INVITES = [
  {
    id: "oinv_qa_docs_open",
    kind: "human",
    created_at: hoursAgo(2),
    created_by: "actor-jordan-human",
  },
  {
    id: "oinv_qa_docs_consumed",
    kind: "human",
    created_at: daysAgo(2),
    created_by: "actor-jordan-human",
    consumed_at: hoursAgo(20),
  },
];

function qaAgent(name, state, overrides = {}) {
  return {
    id: `principal_${name}_agent`,
    ref: `actor:actor-${name}`,
    actor_id: `actor-${name}`,
    host_id: "host_qa_mbp",
    host_slug: "qa-mbp",
    name,
    handle: `${name}.qa-mbp`,
    display_name: `${name} on qa-mbp`,
    identity_kind: "derived",
    state,
    bridge_online: false,
    current_card_ref: null,
    current_card_title: null,
    last_progress_note: null,
    last_progress_at: null,
    active_run: null,
    open_asks_count: 0,
    waiting_ask: null,
    last_signal_at: hoursAgo(1),
    revoked_at: null,
    ...overrides,
  };
}

/** Roster in every derived state (`GET /agents`). */
export const QA_AGENTS = [
  qaAgent("codex", "working", {
    id: "principal_zara_agent",
    actor_id: "actor-zara-ops",
    ref: "actor:actor-zara-ops",
    bridge_online: true,
    current_card_ref: "card:card-launch-checklist",
    current_card_title: "Finalize launch checklist",
    last_progress_note: "Smoke matrix green on staging; rollback wording next.",
    last_progress_at: hoursAgo(0.1),
    active_run: {
      run_id: "run_qa_1",
      adapter: "codex",
      model: "sol",
      duration_seconds: 840,
    },
    last_signal_at: hoursAgo(0.05),
  }),
  qaAgent("claude", "waiting_on_human", {
    actor_id: "actor-soren-release",
    ref: "actor:actor-soren-release",
    open_asks_count: 1,
    waiting_ask: {
      id: "evt-qa-ask-release",
      inbox_item_id: "inbox:ask:thread-qa:evt-qa-ask-release",
      title: "Approve the rollback wording before the release cut",
      severity: "high",
      created_at: hoursAgo(3.2),
      kind: "ask",
      subject_ref: "card:card-launch-checklist",
      subject_title: "Finalize launch checklist",
      requester_actor_id: "actor-soren-release",
    },
  }),
  qaAgent("reviewer", "idle", { last_signal_at: hoursAgo(3) }),
  qaAgent("release-bot", "stale", { last_signal_at: null }),
];

export const QA_HOSTS = [
  {
    id: "host_qa_mbp",
    ref: "host:host_qa_mbp",
    handle: "qa-mbp",
    slug: "qa-mbp",
    display_name: "qa-mbp",
    os_user: "jordan",
    hostname: "qa-mbp.local",
    discovered_adapters: ["claude", "codex", "cursor"],
    key_id: "hkey_qa_mbp",
    excluded_names: ["cursor"],
    agents: QA_AGENTS,
    created_at: daysAgo(30),
    revoked_at: null,
  },
];

export const QA_HOST_ENROLLMENTS = [
  {
    id: "henr_qa_ci",
    user_code: "J6FA-N4XI",
    requested_slug: "ci-runner-3",
    os_user: "runner",
    hostname: "ip-10-0-3-17",
    discovered_adapters: ["generic"],
    adoption_names: [],
    requesting_ip: "203.0.113.17",
    status: "pending",
    expires_at: hoursAgo(-0.1),
    created_at: hoursAgo(0.05),
  },
];

export const QA_AUTH_AUDIT = [
  {
    event_id: "audit_invite_created_qa",
    event_type: "invite_created",
    ts: hoursAgo(2),
    actor_username: "jordan@agentnexus.dev",
    actor_agent_id: QA_AUTH_AGENT.agent_id,
    actor_actor_id: QA_AUTH_AGENT.actor_id,
    invite_id: "oinv_qa_human_onboarding",
  },
  {
    event_id: "audit_invite_consumed_qa",
    event_type: "invite_consumed",
    ts: hoursAgo(20),
    actor_username: "jordan@agentnexus.dev",
    actor_agent_id: QA_AUTH_AGENT.agent_id,
    actor_actor_id: QA_AUTH_AGENT.actor_id,
    subject_username: "iris.docs@agentnexus.dev",
    subject_agent_id: "principal_iris_human",
    subject_actor_id: "actor-iris-docs",
    invite_id: "oinv_qa_docs_consumed",
  },
];

export const QA_SECRETS = [
  {
    id: "secret-openai-prod",
    name: "OPENAI_API_KEY",
    description: "Primary model key for launch workflows",
    updated_at: hoursAgo(6),
  },
  {
    id: "secret-stripe-sandbox",
    name: "STRIPE_SANDBOX_KEY",
    description: "Sandbox billing verification for hosted smoke runs",
    updated_at: hoursAgo(18),
  },
];

export const QA_TOPICS = [
  {
    id: "topic-launch-war-room",
    thread_id: "thread-launch-war-room",
    title: "Launch war room",
    current_summary:
      "Production cutover is green except for an auth callback regression in the mobile shell.",
    summary:
      "Production cutover is green except for an auth callback regression in the mobile shell.",
    state: "active",
    updated_at: hoursAgo(1.5),
    updated_by: "actor-zara-ops",
  },
  {
    id: "topic-billing-rollout",
    thread_id: "thread-billing-rollout",
    title: "Billing rollout dry run",
    current_summary:
      "Stripe checkout and portal flows are passing smoke checks against the hosted sandbox.",
    summary:
      "Stripe checkout and portal flows are passing smoke checks against the hosted sandbox.",
    state: "active",
    updated_at: hoursAgo(5),
    updated_by: "actor-jordan-human",
  },
  {
    id: "topic-docs-refresh",
    thread_id: "thread-docs-refresh",
    title: "Docs refresh for onboarding",
    current_summary:
      "Hosted onboarding copy and screenshots need one final pass before the launch checklist closes.",
    summary:
      "Hosted onboarding copy and screenshots need one final pass before the launch checklist closes.",
    state: "paused",
    updated_at: hoursAgo(18),
    updated_by: "actor-iris-docs",
  },
];

export const QA_BOARDS = [
  {
    id: "board-launch-control",
    title: "Launch control",
    status: "active",
    thread_id: "thread-launch-war-room",
    owners: ["actor-zara-ops", "actor-jordan-human"],
    refs: ["thread:thread-launch-war-room", "document:doc-launch-checklist"],
    document_refs: ["document:doc-launch-checklist"],
    updated_at: hoursAgo(2),
    board_summary: {
      latest_activity_at: hoursAgo(1.5),
      cards_by_column: {
        backlog: 2,
        ready: 3,
        in_progress: 2,
        blocked: 1,
        review: 1,
        done: 5,
      },
    },
    projection_freshness: {
      status: "current",
      generated_at: hoursAgo(1.5),
    },
  },
  {
    id: "board-billing-hardening",
    title: "Billing hardening",
    status: "paused",
    thread_id: "thread-billing-rollout",
    owners: ["actor-jordan-human"],
    refs: ["thread:thread-billing-rollout", "document:doc-billing-runbook"],
    document_refs: ["document:doc-billing-runbook"],
    updated_at: hoursAgo(6),
    board_summary: {
      latest_activity_at: hoursAgo(5),
      cards_by_column: {
        backlog: 1,
        ready: 2,
        in_progress: 1,
        blocked: 0,
        review: 2,
        done: 3,
      },
    },
    projection_freshness: {
      status: "current",
      generated_at: hoursAgo(5),
    },
  },
  {
    id: "board-docs-polish",
    title: "Docs polish",
    status: "active",
    thread_id: "thread-docs-refresh",
    owners: ["actor-iris-docs"],
    refs: ["thread:thread-docs-refresh", "document:doc-onboarding-playbook"],
    document_refs: ["document:doc-onboarding-playbook"],
    updated_at: hoursAgo(12),
    board_summary: {
      latest_activity_at: hoursAgo(12),
      cards_by_column: {
        backlog: 3,
        ready: 1,
        in_progress: 1,
        blocked: 0,
        review: 0,
        done: 2,
      },
    },
    projection_freshness: {
      status: "pending",
      generated_at: hoursAgo(12),
    },
  },
];

export const QA_DOCUMENTS = [
  {
    id: "doc-launch-checklist",
    title: "Launch checklist",
    state: "active",
    thread_id: "thread-launch-war-room",
    head_revision_number: 7,
    updated_at: hoursAgo(1),
    updated_by: "actor-zara-ops",
  },
  {
    id: "doc-billing-runbook",
    title: "Billing runbook",
    state: "active",
    thread_id: "thread-billing-rollout",
    head_revision_number: 4,
    updated_at: hoursAgo(6),
    updated_by: "actor-jordan-human",
  },
  {
    id: "doc-onboarding-playbook",
    title: "Onboarding playbook",
    state: "active",
    thread_id: "thread-docs-refresh",
    head_revision_number: 9,
    updated_at: hoursAgo(16),
    updated_by: "actor-iris-docs",
  },
];

export const QA_ARTIFACTS = [
  {
    id: "artifact-release-review-001",
    kind: "attachment",
    summary: "Cutover review packet",
    created_at: hoursAgo(3),
    created_by: "actor-zara-ops",
    thread_id: "thread-launch-war-room",
    refs: ["thread:thread-launch-war-room", "document:doc-launch-checklist"],
  },
  {
    id: "artifact-billing-receipt-001",
    kind: "attachment",
    summary: "Stripe sandbox smoke receipt",
    created_at: hoursAgo(8),
    created_by: "actor-jordan-human",
    thread_id: "thread-billing-rollout",
    refs: ["thread:thread-billing-rollout", "document:doc-billing-runbook"],
  },
  {
    id: "artifact-docs-evidence-001",
    kind: "attachment",
    summary: "Onboarding screenshot review notes",
    created_at: hoursAgo(20),
    created_by: "actor-iris-docs",
    thread_id: "thread-docs-refresh",
    refs: ["thread:thread-docs-refresh", "document:doc-onboarding-playbook"],
  },
];

export const QA_EVENTS = [
  {
    id: "evt-home-future-safe",
    ts: hoursAgo(30),
    type: "exception_raised",
    actor_id: "actor-iris-docs",
    thread_id: "thread-docs-refresh",
    refs: ["topic:topic-docs-refresh", "document:doc-onboarding-playbook"],
    summary: "Future-safe event type landed for onboarding review follow-up.",
  },
  {
    id: "evt-home-message-launch",
    ts: hoursAgo(9),
    type: "message_posted",
    actor_id: "actor-zara-ops",
    thread_id: "thread-launch-war-room",
    refs: ["thread:thread-launch-war-room", "document:doc-launch-checklist"],
    summary:
      "Launch thread updated with the mobile auth rollback recommendation.",
  },
  {
    id: "evt-home-receipt-billing",
    ts: hoursAgo(8),
    type: "receipt_added",
    actor_id: "actor-jordan-human",
    thread_id: "thread-billing-rollout",
    refs: [
      "thread:thread-billing-rollout",
      "artifact:artifact-billing-receipt-001",
    ],
    summary: "Billing smoke receipt attached to the rollout thread.",
  },
  {
    id: "evt-home-thread-update",
    ts: hoursAgo(5),
    type: "message_posted",
    actor_id: "actor-jordan-human",
    thread_id: "thread-billing-rollout",
    refs: ["topic:topic-billing-rollout", "document:doc-billing-runbook"],
    summary:
      "Billing rollout update posted after the latest portal smoke pass.",
    payload: { changed_fields: ["current_summary", "next_actions"] },
  },
  {
    id: "evt-home-review-launch",
    ts: hoursAgo(3),
    type: "review_completed",
    actor_id: "actor-zara-ops",
    thread_id: "thread-launch-war-room",
    refs: [
      "thread:thread-launch-war-room",
      "artifact:artifact-release-review-001",
    ],
    summary: "Launch cutover review completed with one follow-up action.",
  },
  {
    id: "evt-home-card-moved",
    ts: hoursAgo(2),
    type: "card_moved",
    actor_id: "actor-zara-ops",
    thread_id: "thread-launch-war-room",
    refs: ["board:board-launch-control", "thread:thread-launch-war-room"],
    summary: "Rollback validation card moved into review on Launch control.",
  },
  {
    id: "evt-home-exception-billing",
    ts: hoursAgo(1),
    type: "exception_raised",
    actor_id: "actor-jordan-human",
    thread_id: "thread-billing-rollout",
    refs: ["topic:topic-billing-rollout", "document:doc-billing-runbook"],
    summary: "Legal sign-off gap raised as a billing rollout exception.",
  },
  {
    id: "evt-home-launch-approval",
    ts: hoursAgo(0.75),
    type: "message_posted",
    actor_id: "actor-zara-ops",
    thread_id: "thread-launch-war-room",
    refs: ["thread:thread-launch-war-room", "document:doc-launch-checklist"],
    summary: "Rollback window approved pending the next monitoring sweep.",
  },
  {
    id: "evt-home-human-response-hidden",
    ts: hoursAgo(0.33),
    type: "human_attention_responded",
    actor_id: "actor-jordan-human",
    thread_id: "thread-launch-war-room",
    refs: ["thread:thread-launch-war-room", "inbox:inbox-ask-auth"],
    summary: "Operator responded to the auth rollback ask item.",
  },
];

export const QA_INBOX_POPULATED = [
  {
    id: "inbox-ask-auth",
    kind: "ask",
    category: "action_needed",
    title: "Approve auth callback rollback window",
    subject_ref: "thread:thread-launch-war-room",
    subject_title: "Launch war room",
    thread_id: "thread-launch-war-room",
    related_refs: [
      "thread:thread-launch-war-room",
      "document:doc-launch-checklist",
    ],
    asking_agent_id: "agent-release-orchestrator",
    source_event_time: hoursAgo(10),
    response_proposals: [
      "Hold rollback until next monitoring sweep; ship only if SLO breaches in two consecutive intervals.",
      "Ship rollback immediately; auth regressions exceed the downside of waiting one more telemetry pass.",
      "Split rollout: rollback mobile OAuth first, keep desktop on current build until green board checks.",
    ],
  },
  {
    id: "inbox-tag-billing",
    kind: "tag",
    category: "attention",
    title: "Tag: capture Stripe webhook drift in the runbook",
    subject_ref: "document:doc-billing-runbook",
    subject_title: "Billing runbook",
    thread_id: "thread-billing-rollout",
    related_refs: [
      "thread:thread-billing-rollout",
      "document:doc-billing-runbook",
    ],
    source_event_time: hoursAgo(3),
  },
  {
    id: "inbox-wake-docs",
    kind: "wake",
    category: "attention",
    title: "Wake: re-check onboarding screenshots before Friday",
    subject_ref: "topic:topic-docs-refresh",
    subject_title: "Docs refresh for onboarding",
    thread_id: "thread-docs-refresh",
    related_refs: ["topic:topic-docs-refresh", "thread:thread-docs-refresh"],
    source_event_time: hoursAgo(28),
  },
  {
    id: "inbox-risk-legal",
    kind: "ask",
    category: "risk_exception",
    title: "Legal sign-off missing for the billing terms copy",
    subject_ref: "topic:topic-billing-rollout",
    subject_title: "Billing rollout dry run",
    thread_id: "thread-billing-rollout",
    related_refs: [
      "topic:topic-billing-rollout",
      "document:doc-billing-runbook",
    ],
    asking_agent_id: "agent-billing-watch",
    source_event_time: hoursAgo(1),
    response_proposals: [
      "Pause rollout until Legal posts approved terms wording; rerun dry-run with updated copy snapshot.",
      "Proceed dry-run with bold ‘draft’ watermark and block production traffic until Legal signs.",
      "Narrow rollout to sandbox orgs only until counsel signs billing terms canonical version.",
    ],
  },
];

export const QA_ASK_ITEM = {
  id: "inbox-ask-auth",
  kind: "ask",
  category: "action_needed",
  title: "Approve auth callback rollback window",
  body: "Can we hold the mobile auth callback rollback until the next monitoring sweep, or should I ship the rollback immediately?",
  query_text:
    "Can we hold the mobile auth callback rollback until the next monitoring sweep, or should I ship the rollback immediately?",
  subject_ref: "thread:thread-launch-war-room",
  related_refs: [
    "thread:thread-launch-war-room",
    "document:doc-launch-checklist",
  ],
  thread_id: "thread-launch-war-room",
  asking_agent_id: "agent-release-orchestrator",
  coverage_hint: "partial",
  source_event_time: hoursAgo(10),
  response_proposals: [
    "Hold rollback until next monitoring sweep; ship only if SLO breaches in two consecutive intervals.",
    "Ship rollback immediately; auth regressions exceed the downside of waiting one more telemetry pass.",
    "Split rollout: rollback mobile OAuth first, keep desktop on current build until green board checks.",
  ],
};

export function filterByQuery(items, query, fields) {
  const normalizedQuery = String(query ?? "")
    .trim()
    .toLowerCase();
  if (!normalizedQuery) {
    return [...items];
  }

  return items.filter((item) =>
    fields.some((field) => {
      const value = item?.[field];
      if (Array.isArray(value)) {
        return value.some((entry) =>
          String(entry ?? "")
            .toLowerCase()
            .includes(normalizedQuery),
        );
      }
      return String(value ?? "")
        .toLowerCase()
        .includes(normalizedQuery);
    }),
  );
}
