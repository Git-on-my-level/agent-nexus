/** Presentation only. Core owns work, freshness policy and every durable transition. */
export const PHASES = [
  "backlog",
  "ready",
  "in_progress",
  "blocked",
  "review",
  "done",
  "cancelled",
  "unknown",
];
export const PHASE_LABELS = {
  backlog: "Backlog",
  ready: "Ready",
  in_progress: "In progress",
  blocked: "Blocked",
  review: "In review",
  done: "Done",
  cancelled: "Cancelled",
  unknown: "Unknown",
};
export const label = (value) =>
  PHASE_LABELS[value] || String(value || "Unknown");

export function safeSourceHref(value) {
  try {
    const url = new URL(value);
    return ["http:", "https:"].includes(url.protocol) &&
      !url.username &&
      !url.password
      ? url.href
      : "";
  } catch {
    return "";
  }
}

export function freshness(
  { observedAt, staleAfter, error, status } = {},
  now = Date.now(),
) {
  if (error || status === "error")
    return { key: "error", label: "Refresh failed", tone: "warn" };
  const observed = Date.parse(observedAt);
  if (!Number.isFinite(observed) || observed > now + 60_000)
    return { key: "unknown", label: "Freshness unknown", tone: "neutral" };
  const deadline = Date.parse(staleAfter);
  if (status === "stale" || (Number.isFinite(deadline) && deadline <= now))
    return { key: "stale", label: "Stale evidence", tone: "warn" };
  if (status === "fresh" || Number.isFinite(deadline))
    return { key: "fresh", label: "Fresh observation", tone: "ok" };
  return { key: "unknown", label: "Freshness unknown", tone: "neutral" };
}

export function workFreshness(work, now = Date.now()) {
  const f = work?.freshness ?? {};
  const observed = Date.parse(f.last_observed_at);
  const seconds = Number(f.stale_after_seconds);
  const deadline =
    Number.isFinite(observed) && seconds > 0
      ? new Date(observed + seconds * 1000).toISOString()
      : undefined;
  return freshness(
    {
      observedAt: f.last_observed_at,
      staleAfter: deadline,
      status: f.status,
      error: work?.refresh?.last_error,
    },
    now,
  );
}

export function receiptSignal(state) {
  const states = {
    awaiting_answer: ["Needs your decision", "warn"],
    answered: ["Answered", "neutral"],
    pending_delivery: ["Pending delivery", "warn"],
    pending: ["Pending", "warn"],
    queued: ["Queued", "neutral"],
    delivered: ["Delivered", "neutral"],
    acknowledged: ["Acknowledged", "neutral"],
    applied: ["Applied; verification pending", "neutral"],
    source_reported: ["Source reported; not independently verified", "neutral"],
    sending: ["Delivery in progress", "neutral"],
    verified: ["Outcome verified", "ok"],
    failed: ["Failed", "danger"],
    unknown: ["Delivery uncertain", "warn"],
    superseded: ["Superseded", "neutral"],
  };
  const [display, tone] = states[state] ?? [
    String(state || "Unknown"),
    "neutral",
  ];
  return { label: display, tone, verified: state === "verified" };
}

export function phaseGroups(records) {
  const keys = [
    ...PHASES.filter((phase) => phase !== "unknown"),
    ...new Set(
      records
        .map((work) => work.phase || "unknown")
        .filter((phase) => !PHASES.includes(phase)),
    ),
    "unknown",
  ];
  return keys.map((key) => ({
    key,
    label: label(key),
    items: records.filter((work) => (work.phase || "unknown") === key),
  }));
}

export function filterWork(records, filters = {}, now = Date.now()) {
  const query = String(filters.q || "")
    .toLowerCase()
    .trim();
  return records.filter(
    (work) =>
      (!query ||
        [
          work.title,
          work.summary,
          work.ref,
          work.source?.native_id,
          work.next_action,
        ].some((value) =>
          String(value || "")
            .toLowerCase()
            .includes(query),
        )) &&
      (!filters.source || work.source?.authority === filters.source) &&
      (!filters.project_ref || work.project_ref === filters.project_ref) &&
      (!filters.owner || work.owner === filters.owner) &&
      (!filters.phase || work.phase === filters.phase) &&
      (!filters.freshness ||
        workFreshness(work, now).key === filters.freshness),
  );
}

export function workKey(work) {
  return work.ref || work.handle || work.id;
}
export function sourceLabel(source) {
  return (
    {
      nexus: "Nexus",
      github: "GitHub",
      multica: "Multica",
      git: "Git",
      ssh_git: "Git over SSH",
    }[source?.authority] ||
    source?.authority ||
    "Authority unknown"
  );
}
export function errorMessage(error) {
  return error instanceof Error
    ? error.message
    : String(error || "Unable to load workspace data.");
}
