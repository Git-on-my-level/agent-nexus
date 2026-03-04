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
    key_artifacts: ["artifact-policy-draft"],
    cadence: "weekly",
    current_summary:
      "Cross-functional onboarding handoff is delayed by policy review.",
    next_actions: ["Confirm legal signer", "Publish revised checklist"],
    open_commitments: ["commitment-onboard-1"],
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
    key_artifacts: [],
    cadence: "daily",
    current_summary: "Postmortem incomplete due to missing external logs.",
    next_actions: ["Collect provider logs", "Draft postmortem"],
    open_commitments: ["commitment-sla-42"],
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
  {
    id: "evt-1003",
    ts: "2026-03-03T11:20:00.000Z",
    type: "snapshot_updated",
    actor_id: "actor-policy-owner",
    thread_id: "thread-onboarding",
    refs: ["thread:thread-onboarding"],
    summary: "Updated thread status and summary.",
    payload: {
      changed_fields: ["status", "current_summary"],
    },
    provenance: {
      sources: ["actor_statement:event-1003"],
      by_field: {
        status: ["receipt:artifact-334"],
      },
    },
  },
];

const commitments = [
  {
    id: "commitment-onboard-1",
    thread_id: "thread-onboarding",
    title: "Finalize onboarding policy exceptions",
    owner: "actor-policy-owner",
    due_at: "2026-03-10T00:00:00.000Z",
    status: "open",
    definition_of_done: [
      "Policy exception flow approved",
      "Escalation path documented",
    ],
    links: ["thread:thread-onboarding", "artifact:artifact-policy-draft"],
    updated_at: "2026-03-03T13:00:00.000Z",
    updated_by: "actor-policy-owner",
    provenance: {
      sources: ["actor_statement:event-1001"],
    },
  },
  {
    id: "commitment-sla-42",
    thread_id: "thread-incident-42",
    title: "Restore incident SLA compliance",
    owner: "actor-integrations",
    due_at: "2026-03-08T00:00:00.000Z",
    status: "blocked",
    definition_of_done: ["Provider logs attached", "Postmortem published"],
    links: ["thread:thread-incident-42"],
    updated_at: "2026-03-03T15:00:00.000Z",
    updated_by: "actor-integrations",
    provenance: {
      sources: ["inferred"],
      by_field: {
        status: ["inferred"],
      },
    },
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

function isOpenCommitmentStatus(status) {
  const normalized = String(status ?? "").trim();
  return normalized !== "done" && normalized !== "canceled";
}

function normalizeRefList(value) {
  if (!Array.isArray(value)) {
    return [];
  }

  return value.map((item) => String(item).trim()).filter(Boolean);
}

function commitmentHasRequiredStatusRef(status, refs) {
  const prefixes = normalizeRefList(refs).map(
    (ref) => String(ref).split(":")[0],
  );

  if (status === "done") {
    return prefixes.includes("artifact") || prefixes.includes("event");
  }

  if (status === "canceled") {
    return prefixes.includes("event");
  }

  return true;
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
    key_artifacts: Array.isArray(thread.key_artifacts)
      ? thread.key_artifacts
      : [],
    next_actions: Array.isArray(thread.next_actions) ? thread.next_actions : [],
    open_commitments: Array.isArray(thread.open_commitments)
      ? thread.open_commitments
      : [],
  };

  threads.unshift(created);
  return created;
}

export function getMockThread(threadId) {
  return threads.find((thread) => thread.id === threadId) ?? null;
}

function updateThreadOpenCommitments({ thread_id, commitment_id, status }) {
  const thread = getMockThread(thread_id);
  if (!thread) {
    return;
  }

  const openCommitments = Array.isArray(thread.open_commitments)
    ? [...thread.open_commitments]
    : [];
  const existingIndex = openCommitments.findIndex((id) => id === commitment_id);
  const shouldBeOpen = isOpenCommitmentStatus(status);

  if (shouldBeOpen && existingIndex === -1) {
    openCommitments.push(commitment_id);
  }

  if (!shouldBeOpen && existingIndex >= 0) {
    openCommitments.splice(existingIndex, 1);
  }

  thread.open_commitments = openCommitments;
}

export function updateMockThread({
  actor_id,
  thread_id,
  patch = {},
  if_updated_at,
}) {
  const thread = getMockThread(thread_id);
  if (!thread) {
    return { error: "not_found" };
  }

  if (
    if_updated_at &&
    String(if_updated_at) !== String(thread.updated_at ?? "")
  ) {
    return { error: "conflict", current: thread };
  }

  const next = { ...thread };

  for (const [field, value] of Object.entries(patch)) {
    if (field === "open_commitments") {
      continue;
    }

    if (
      field === "tags" ||
      field === "next_actions" ||
      field === "key_artifacts"
    ) {
      next[field] = Array.isArray(value)
        ? value.map((item) => String(item).trim()).filter(Boolean)
        : [];
      continue;
    }

    next[field] = value;
  }

  next.updated_at = new Date().toISOString();
  next.updated_by = actor_id;

  const index = threads.findIndex((candidate) => candidate.id === thread_id);
  threads[index] = next;

  return { thread: next };
}

export function listMockCommitments(filters = {}) {
  return commitments.filter((commitment) => {
    if (
      filters.thread_id &&
      String(commitment.thread_id) !== String(filters.thread_id)
    ) {
      return false;
    }

    if (filters.owner && String(commitment.owner) !== String(filters.owner)) {
      return false;
    }

    if (
      filters.status &&
      String(commitment.status) !== String(filters.status)
    ) {
      return false;
    }

    if (
      filters.due_before &&
      Date.parse(String(commitment.due_at)) >
        Date.parse(String(filters.due_before))
    ) {
      return false;
    }

    if (
      filters.due_after &&
      Date.parse(String(commitment.due_at)) <
        Date.parse(String(filters.due_after))
    ) {
      return false;
    }

    return true;
  });
}

export function getMockCommitment(commitmentId) {
  return (
    commitments.find((commitment) => commitment.id === commitmentId) ?? null
  );
}

export function createMockCommitment({ actor_id, commitment }) {
  const created = {
    id: `commitment-${Math.random().toString(36).slice(2, 10)}`,
    thread_id: commitment.thread_id,
    title: commitment.title,
    owner: commitment.owner,
    due_at: commitment.due_at,
    status: commitment.status ?? "open",
    definition_of_done: Array.isArray(commitment.definition_of_done)
      ? commitment.definition_of_done
          .map((item) => String(item).trim())
          .filter(Boolean)
      : [],
    links: normalizeRefList(commitment.links),
    updated_at: new Date().toISOString(),
    updated_by: actor_id,
    provenance: commitment.provenance ?? {
      sources: ["actor_statement:ui"],
    },
  };

  commitments.unshift(created);
  updateThreadOpenCommitments({
    thread_id: created.thread_id,
    commitment_id: created.id,
    status: created.status,
  });

  return created;
}

export function updateMockCommitment({
  actor_id,
  commitment_id,
  patch = {},
  refs = [],
  if_updated_at,
}) {
  const commitment = getMockCommitment(commitment_id);
  if (!commitment) {
    return { error: "not_found" };
  }

  if (
    if_updated_at &&
    String(if_updated_at) !== String(commitment.updated_at ?? "")
  ) {
    return { error: "conflict", current: commitment };
  }

  const next = { ...commitment };

  for (const [field, value] of Object.entries(patch)) {
    if (field === "definition_of_done" || field === "links") {
      next[field] = Array.isArray(value)
        ? value.map((item) => String(item).trim()).filter(Boolean)
        : [];
      continue;
    }

    next[field] = value;
  }

  const statusChanged =
    Object.prototype.hasOwnProperty.call(patch, "status") &&
    String(next.status) !== String(commitment.status);

  if (
    statusChanged &&
    (String(next.status) === "done" || String(next.status) === "canceled") &&
    !commitmentHasRequiredStatusRef(String(next.status), refs)
  ) {
    return {
      error: "invalid_transition",
      message:
        String(next.status) === "done"
          ? "status=done requires artifact:<receipt_id> or event:<decision_event_id> in refs."
          : "status=canceled requires event:<decision_event_id> in refs.",
    };
  }

  if (
    statusChanged &&
    (String(next.status) === "done" || String(next.status) === "canceled")
  ) {
    const statusRefs = normalizeRefList(refs);
    next.provenance = {
      ...(next.provenance ?? { sources: [] }),
      by_field: {
        ...((next.provenance ?? {}).by_field ?? {}),
        status: statusRefs,
      },
    };
  }

  next.updated_at = new Date().toISOString();
  next.updated_by = actor_id;

  const index = commitments.findIndex(
    (candidate) => candidate.id === commitment_id,
  );
  commitments[index] = next;

  updateThreadOpenCommitments({
    thread_id: next.thread_id,
    commitment_id: next.id,
    status: next.status,
  });

  return { commitment: next };
}
