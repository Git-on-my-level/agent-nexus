const actors = [];
const now = Date.now();

const threads = [
  {
    id: "thread-onboarding",
    type: "process",
    title: "Customer Onboarding Workflow",
    status: "active",
    priority: "p1",
    tags: ["ops", "customer", "compliance"],
    cadence: "weekly",
    current_summary:
      "Cross-functional onboarding handoff is delayed by policy review.",
    next_actions: ["Confirm legal signer", "Publish revised checklist"],
    next_check_in_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 6 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-policy-owner",
    provenance: {
      sources: ["actor_statement:event-1001", "receipt:artifact-334"],
    },
  },
  {
    id: "thread-incident-42",
    type: "incident",
    title: "Incident Follow-up",
    status: "paused",
    priority: "p0",
    tags: ["incident", "infra"],
    cadence: "daily",
    current_summary: "Postmortem incomplete due to missing external logs.",
    next_actions: ["Collect provider logs", "Draft postmortem"],
    next_check_in_at: new Date(now + 1 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 1 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-integrations",
    provenance: {
      sources: ["inferred"],
      notes: "Thread status inferred from unresolved commitments.",
    },
  },
];

const inboxItems = [
  {
    id: "inbox-001",
    category: "decision_needed",
    title: "Approve onboarding exception handling",
    recommended_action: "Record a decision on escalation path.",
    thread_id: "thread-onboarding",
    commitment_id: "commitment-onboard-1",
    refs: [
      "thread:thread-onboarding",
      "artifact:artifact-policy-draft",
      "url:https://example.com/onboarding-brief",
    ],
    source_event_time: "2026-03-03T12:00:00.000Z",
  },
  {
    id: "inbox-002",
    category: "exception",
    title: "Missing legal signer for external policy packet",
    recommended_action: "Acknowledge and assign remediation owner.",
    thread_id: "thread-onboarding",
    refs: ["thread:thread-onboarding", "event:evt-1001"],
    source_event_time: "2026-03-03T09:00:00.000Z",
  },
  {
    id: "inbox-003",
    category: "commitment_risk",
    title: "Commitment at risk: onboarding SLA response",
    recommended_action: "Confirm revised due date with owner.",
    thread_id: "thread-incident-42",
    commitment_id: "commitment-sla-42",
    refs: ["thread:thread-incident-42"],
    source_event_time: "2026-03-02T15:20:00.000Z",
  },
];

const events = [
  {
    id: "evt-1001",
    ts: "2026-03-03T08:00:00.000Z",
    type: "message_posted",
    actor_id: "actor-policy-owner",
    thread_id: "thread-onboarding",
    refs: ["thread:thread-onboarding", "artifact:artifact-policy-draft"],
    summary: "Waiting on legal review confirmation.",
    payload: { text: "Need legal signer to proceed." },
    provenance: { sources: ["actor_statement:event-1001"] },
  },
  {
    id: "evt-1002",
    ts: "2026-03-03T10:30:00.000Z",
    type: "unknown_future_type",
    actor_id: "actor-integrations",
    thread_id: "thread-onboarding",
    refs: ["event:evt-1001", "mystery:opaque-value"],
    summary: "Future event type should still render.",
    payload: { score: 9 },
    provenance: { sources: ["inferred"] },
  },
];

export function listMockActors() {
  return actors;
}

export function createMockActor(actor) {
  actors.push(actor);
  return actor;
}

export function createMockEvent(event) {
  events.push(event);
  return event;
}

export function listMockInboxItems() {
  return inboxItems;
}

export function ackMockInboxItem({ thread_id, inbox_item_id }) {
  const index = inboxItems.findIndex(
    (item) =>
      item.id === inbox_item_id &&
      (!thread_id || String(item.thread_id) === String(thread_id)),
  );

  if (index === -1) {
    return null;
  }

  return inboxItems.splice(index, 1)[0];
}

export function listMockTimelineEvents(threadId) {
  return events
    .filter((event) => event.thread_id === threadId)
    .sort((a, b) => String(b.ts).localeCompare(String(a.ts)));
}

function isThreadStale(thread) {
  if (!thread.next_check_in_at) {
    return false;
  }

  return Date.parse(String(thread.next_check_in_at)) < Date.now();
}

function normalizeTagFilters(tag) {
  if (tag === undefined || tag === null || tag === "") {
    return [];
  }

  if (Array.isArray(tag)) {
    return tag.map((value) => String(value));
  }

  return String(tag)
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
}

export function listMockThreads(filters = {}) {
  const tagFilters = normalizeTagFilters(filters.tag);
  const staleFilter =
    filters.stale === undefined ? undefined : String(filters.stale) === "true";

  return threads.filter((thread) => {
    if (filters.status && String(thread.status) !== String(filters.status)) {
      return false;
    }

    if (
      filters.priority &&
      String(thread.priority) !== String(filters.priority)
    ) {
      return false;
    }

    if (filters.cadence && String(thread.cadence) !== String(filters.cadence)) {
      return false;
    }

    if (tagFilters.length > 0) {
      const hasTagMatch = tagFilters.every((tag) => thread.tags?.includes(tag));
      if (!hasTagMatch) {
        return false;
      }
    }

    if (staleFilter !== undefined && isThreadStale(thread) !== staleFilter) {
      return false;
    }

    return true;
  });
}

export function createMockThread({ actor_id, thread }) {
  const created = {
    id: `thread-${Math.random().toString(36).slice(2, 10)}`,
    updated_at: new Date().toISOString(),
    updated_by: actor_id,
    provenance: {
      sources: ["actor_statement:ui"],
    },
    ...thread,
    tags: Array.isArray(thread.tags) ? thread.tags : [],
  };

  threads.unshift(created);
  return created;
}

export function getMockThread(threadId) {
  return threads.find((thread) => thread.id === threadId) ?? null;
}
