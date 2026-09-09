import { formatTimestamp } from "$lib/formatDate";

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

/**
 * Freshness as a reader would say it.
 *
 * The keys and tones are unchanged — callers filter on `.key` — but the labels
 * were internal vocabulary ("Fresh observation", "Stale evidence", "Freshness
 * unknown", "Refresh failed"), which describes our collection pipeline rather
 * than answering the reader's question: when did we last look, and can we
 * still reach the thing we looked at.
 */
export function freshness(
  { observedAt, staleAfter, error, status, sourceName } = {},
  now = Date.now(),
) {
  const named = String(sourceName ?? "").trim();
  if (error || status === "error")
    return {
      key: "error",
      label: named ? `Can't reach ${named}` : "Can't reach source",
      tone: "warn",
    };
  const observed = Date.parse(observedAt);
  if (!Number.isFinite(observed) || observed > now + 60_000)
    return { key: "unknown", label: "Never checked", tone: "neutral" };
  const deadline = Date.parse(staleAfter);
  if (status === "stale" || (Number.isFinite(deadline) && deadline <= now))
    return { key: "stale", label: "Not checked lately", tone: "warn" };
  if (status === "fresh" || Number.isFinite(deadline)) {
    const when = formatTimestamp(observedAt);
    return {
      key: "fresh",
      label: when ? `Checked ${when}` : "Checked recently",
      tone: "ok",
    };
  }
  return { key: "unknown", label: "Never checked", tone: "neutral" };
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
      sourceName: work?.source ? sourceLabel(work.source) : "",
    },
    now,
  );
}

const RECEIPT_PRIMARY = {
  awaiting_answer: ["Needs you", "warn", "needs_you"],
  delivered: ["Delivered", "neutral", "delivered"],
  verified: ["Done", "ok", "done"],
  failed: ["Failed", "danger", "failed"],
};

const RECEIPT_FOLDED = {
  answered: "Answered",
  pending_delivery: "Pending delivery",
  acknowledged: "Acknowledged",
  source_reported: "Source reported; not independently verified",
  sending: "Delivery in progress",
  unknown: "Delivery uncertain",
  superseded: "Superseded",
};

export function receiptSignal(state) {
  const primary = RECEIPT_PRIMARY[state];
  if (primary) {
    const [display, tone, key] = primary;
    return {
      label: display,
      tone,
      key,
      primary: true,
      verified: state === "verified",
    };
  }
  return {
    label: RECEIPT_FOLDED[state] || String(state || "Unknown"),
    tone: "neutral",
    key: "other",
    primary: false,
    verified: false,
  };
}

const DECISION_SUMMARY_KEYS = [
  "summary",
  "title",
  "question",
  "next_action",
  "action",
  "instruction",
  "message",
  "reason",
];

function parseStructuredInstruction(text) {
  const trimmed = String(text ?? "").trim();
  if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) return null;
  try {
    const value = JSON.parse(trimmed);
    return typeof value === "object" && value !== null ? value : null;
  } catch {
    return null;
  }
}

/**
 * Human title for a decision. PM harnesses sometimes file the instruction as
 * a JSON payload (`{"next_action":"…"}`); a JSON blob is never a title —
 * prefer an instruction summary field, then the task title, then a generic.
 */
function sentenceCase(text) {
  const s = String(text ?? "").trim();
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : s;
}

export function decisionTitle(item, taskTitle = "") {
  return sentenceCase(decisionTitleRaw(item, taskTitle));
}

function decisionTitleRaw(item, taskTitle = "") {
  const instruction = String(item?.instruction ?? "").trim();
  const structured = parseStructuredInstruction(instruction);
  if (structured) {
    if (!Array.isArray(structured)) {
      for (const key of DECISION_SUMMARY_KEYS) {
        const value = structured[key];
        if (typeof value === "string" && value.trim()) return value.trim();
      }
      for (const value of Object.values(structured)) {
        if (typeof value === "string" && value.trim()) return value.trim();
      }
    }
    return taskTitle || "Proposed decision";
  }
  return instruction || taskTitle || "Decision";
}

/**
 * Structured instruction payload to show under a disclosure. Empty for
 * plain-text instructions — the text itself is the title, not a payload.
 */
export function decisionPayload(item) {
  const structured = parseStructuredInstruction(item?.instruction);
  if (!structured) return "";
  try {
    return JSON.stringify(structured, null, 2);
  } catch {
    return String(item?.instruction ?? "").trim();
  }
}

export function isNexusOwned(work) {
  return String(work?.source?.authority ?? "").toLowerCase() === "nexus";
}

export function taskDetailPath(work) {
  return `/tasks/${encodeURIComponent(workKey(work))}`;
}

export function cardIdFromWork(work) {
  const id = String(work?.id ?? "").trim();
  if (id) return id;
  const ref = String(work?.ref ?? work?.handle ?? "").trim();
  if (ref.startsWith("card:")) return ref.slice("card:".length);
  return ref;
}

export function workTargetRevision(work) {
  return String(
    work?.source?.revision ||
      work?.freshness?.source_revision ||
      work?.version ||
      "0",
  );
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
  const raw =
    error instanceof Error
      ? error.message
      : String(error || "Unable to load workspace data.");
  if (/capacity reached/i.test(raw)) {
    return "The PM is busy with other questions. Your message is kept; try again in a minute.";
  }
  if (/PM permission denied/i.test(raw)) {
    return "You are not signed in as someone who can use the PM. Sign in again and retry.";
  }
  return raw;
}
