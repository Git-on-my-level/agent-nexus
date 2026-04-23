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

function hoursFromNow(hours) {
  return atOffsetMs(hours * 60 * 60 * 1000);
}

export const QA_HOSTED_ACCOUNT = {
  id: "acct_qa_jordan",
  email: "jordan@agentnexus.dev",
  display_name: "Jordan Kim",
};

export const QA_HOSTED_ORGS = [
  {
    id: "org_qa_primary",
    slug: "northwind-labs",
    display_name: "Northwind Autonomy",
    plan_tier: "team",
    status: "active",
    created_at: daysAgo(60),
    updated_at: hoursAgo(5),
  },
  {
    id: "org_qa_sidecar",
    slug: "solstice-ops",
    display_name: "Solstice Ops",
    plan_tier: "starter",
    status: "active",
    created_at: daysAgo(14),
    updated_at: hoursAgo(12),
  },
];

export const QA_HOSTED_WORKSPACES = [
  {
    id: "ws_qa_orbit",
    organization_id: "org_qa_primary",
    slug: "orbit",
    display_name: "Orbit Release",
    status: "ready",
    created_at: daysAgo(30),
    updated_at: hoursAgo(2),
  },
  {
    id: "ws_qa_docs",
    organization_id: "org_qa_primary",
    slug: "docs",
    display_name: "Docs Studio",
    status: "ready",
    created_at: daysAgo(18),
    updated_at: hoursAgo(8),
  },
  {
    id: "ws_qa_staging",
    organization_id: "org_qa_primary",
    slug: "staging",
    display_name: "Staging Fleet",
    status: "provisioning",
    created_at: hoursAgo(10),
    updated_at: hoursAgo(1),
  },
];

export const QA_HOSTED_BILLING_SUMMARY = {
  organization_id: "org_qa_primary",
  plan_tier: "team",
  billing_account: {
    organization_id: "org_qa_primary",
    provider: "stripe",
    billing_status: "active",
    stripe_customer_id: "cus_qa_team",
    stripe_subscription_id: "sub_qa_team",
    stripe_price_id: "price_qa_team",
    stripe_subscription_status: "active",
    current_period_end: atOffsetMs(18 * 24 * 60 * 60 * 1000),
    cancel_at_period_end: false,
    last_webhook_event_id: "evt_qa_billing_sync",
    last_webhook_event_type: "customer.subscription.updated",
    last_webhook_received_at: hoursAgo(2),
    created_at: daysAgo(30),
    updated_at: hoursAgo(2),
  },
  usage_summary: {
    organization_id: "org_qa_primary",
    plan: {
      id: "team",
      display_name: "Pro",
      workspace_limit: 5,
      max_artifacts_per_workspace: 125000,
      artifact_capacity: 125000,
      included_storage_gb: 25,
    },
    usage: {
      workspace_count: 3,
      artifact_count: 384,
      storage_gb: 7.4,
      monthly_launch_count: 118,
    },
    quota: {
      workspaces_remaining: 2,
      artifacts_remaining: 124616,
      storage_gb_remaining: 17.6,
    },
    workspaces: QA_HOSTED_WORKSPACES.map((workspace, index) => ({
      workspace_id: workspace.id,
      workspace_slug: workspace.slug,
      workspace_name: workspace.display_name,
      artifact_count: [164, 102, 118][index] ?? 0,
      storage_gb: [3.2, 1.5, 2.7][index] ?? 0,
      last_active_at:
        [hoursAgo(2), hoursAgo(8), hoursAgo(1)][index] ?? hoursAgo(1),
    })),
  },
  configuration: {
    provider: "stripe",
    configured: true,
    publishable_key_configured: true,
    secret_key_configured: true,
    webhook_secret_configured: true,
    checkout_configured: true,
    customer_portal_configured: true,
    plan_price_ids: {
      starter: "price_qa_starter",
      team: "price_qa_team",
      scale: "price_qa_scale",
    },
    missing_configuration: [],
  },
};

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
  QA_AUTH_AGENT,
  {
    agent_id: "principal_zara_agent",
    actor_id: "actor-zara-ops",
    username: "zara@agents.internal",
    principal_kind: "agent",
    auth_method: "agent_key",
  },
];

export const QA_INVITES = [
  {
    id: "oinv_qa_agent_launch",
    kind: "agent",
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

export const QA_AUTH_AUDIT = [
  {
    event_id: "audit_invite_created_qa",
    event_type: "invite_created",
    ts: hoursAgo(2),
    actor_username: QA_HOSTED_ACCOUNT.email,
    actor_agent_id: QA_AUTH_AGENT.agent_id,
    actor_actor_id: QA_AUTH_AGENT.actor_id,
    invite_id: "oinv_qa_agent_launch",
  },
  {
    event_id: "audit_invite_consumed_qa",
    event_type: "invite_consumed",
    ts: hoursAgo(20),
    actor_username: QA_HOSTED_ACCOUNT.email,
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
    type: "incident",
    title: "Launch war room",
    current_summary:
      "Production cutover is green except for an auth callback regression in the mobile shell.",
    summary:
      "Production cutover is green except for an auth callback regression in the mobile shell.",
    status: "active",
    priority: "p0",
    cadence: "daily",
    tags: ["launch", "incident", "critical"],
    updated_at: hoursAgo(1.5),
    updated_by: "actor-zara-ops",
    next_check_in_at: hoursAgo(2),
  },
  {
    id: "topic-billing-rollout",
    thread_id: "thread-billing-rollout",
    type: "initiative",
    title: "Billing rollout dry run",
    current_summary:
      "Stripe checkout and portal flows are passing smoke checks against the hosted sandbox.",
    summary:
      "Stripe checkout and portal flows are passing smoke checks against the hosted sandbox.",
    status: "active",
    priority: "p1",
    cadence: "weekly",
    tags: ["billing", "launch"],
    updated_at: hoursAgo(5),
    updated_by: "actor-jordan-human",
    next_check_in_at: hoursFromNow(20),
  },
  {
    id: "topic-docs-refresh",
    thread_id: "thread-docs-refresh",
    type: "request",
    title: "Docs refresh for onboarding",
    current_summary:
      "Hosted onboarding copy and screenshots need one final pass before the launch checklist closes.",
    summary:
      "Hosted onboarding copy and screenshots need one final pass before the launch checklist closes.",
    status: "paused",
    priority: "p2",
    cadence: "weekly",
    tags: ["docs", "onboarding"],
    updated_at: hoursAgo(18),
    updated_by: "actor-iris-docs",
    next_check_in_at: hoursFromNow(36),
  },
];

export const QA_BOARDS = [
  {
    id: "board-launch-control",
    title: "Launch control",
    status: "active",
    thread_id: "thread-launch-war-room",
    labels: ["launch", "ops"],
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
    labels: ["billing", "qa"],
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
    labels: ["docs"],
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
    labels: ["launch", "ops"],
    state: "active",
    thread_id: "thread-launch-war-room",
    head_revision_number: 7,
    updated_at: hoursAgo(1),
    updated_by: "actor-zara-ops",
  },
  {
    id: "doc-billing-runbook",
    title: "Billing runbook",
    labels: ["billing", "runbook"],
    state: "active",
    thread_id: "thread-billing-rollout",
    head_revision_number: 4,
    updated_at: hoursAgo(6),
    updated_by: "actor-jordan-human",
  },
  {
    id: "doc-onboarding-playbook",
    title: "Onboarding playbook",
    labels: ["docs", "onboarding"],
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
    kind: "review",
    summary: "Cutover review packet",
    created_at: hoursAgo(3),
    created_by: "actor-zara-ops",
    thread_id: "thread-launch-war-room",
    refs: ["thread:thread-launch-war-room", "document:doc-launch-checklist"],
  },
  {
    id: "artifact-billing-receipt-001",
    kind: "receipt",
    summary: "Stripe sandbox smoke receipt",
    created_at: hoursAgo(8),
    created_by: "actor-jordan-human",
    thread_id: "thread-billing-rollout",
    refs: ["thread:thread-billing-rollout", "document:doc-billing-runbook"],
  },
  {
    id: "artifact-docs-evidence-001",
    kind: "evidence",
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
    type: "future_signal_emitted",
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
    refs: [
      "thread:thread-launch-war-room",
      "document:doc-launch-checklist",
    ],
    summary: "Launch thread updated with the mobile auth rollback recommendation.",
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
    type: "thread_updated",
    actor_id: "actor-jordan-human",
    thread_id: "thread-billing-rollout",
    refs: [
      "topic:topic-billing-rollout",
      "document:doc-billing-runbook",
    ],
    summary: "Billing rollout summary updated after the latest portal smoke pass.",
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
    id: "evt-home-decision-launch",
    ts: hoursAgo(0.75),
    type: "decision_made",
    actor_id: "actor-zara-ops",
    thread_id: "thread-launch-war-room",
    refs: [
      "thread:thread-launch-war-room",
      "document:doc-launch-checklist",
    ],
    summary: "Rollback window approved pending the next monitoring sweep.",
  },
  {
    id: "evt-home-ack-hidden",
    ts: hoursAgo(0.33),
    type: "inbox_item_acknowledged",
    actor_id: "actor-jordan-human",
    thread_id: "thread-launch-war-room",
    refs: [
      "thread:thread-launch-war-room",
      "inbox:inbox-ask-auth",
    ],
    summary: "Operator acknowledged the auth rollback ask item.",
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
  },
];

export const QA_ASK_ITEM = {
  id: "inbox-ask-auth",
  kind: "ask",
  category: "action_needed",
  title: "Approve auth callback rollback window",
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
