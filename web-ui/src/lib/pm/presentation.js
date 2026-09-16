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
// Not every failed read is unreachability; a reader that is not ready or a
// credential problem is local, and the badge should not blame the source.
const READ_ERROR_LABELS = {
  policy_denied: "Reader not ready",
  isolation_unavailable: "Reader not ready",
  configuration: "Reader not configured",
  invalid_output: "Reader output invalid",
  rate_limited: "Rate limited",
  permission: "Access denied",
  not_found: "Not found at source",
};

// Why a read failed, for a task page: the label above says what, this says
// what it means and what changes it.
const READ_ERROR_EXPLANATIONS = {
  policy_denied:
    "The reader for this source has no approved version yet, so nothing is read until an operator activates one (anx-observe jit-activate; see the runbook).",
  isolation_unavailable:
    "The sandbox that runs readers is not available on this host, so nothing is read.",
  configuration: "The reader for this source is not configured.",
  invalid_output: "The reader ran, but its output could not be understood.",
  rate_limited:
    "The source is rate limiting reads; the last good read is kept until it allows another.",
  permission: "The source refused access with the credentials configured.",
  not_found: "The source no longer has this item.",
};

export function readErrorExplanation(error) {
  if (!error) return "";
  if (typeof error === "string") return error;
  const code = String(error.code ?? "").trim();
  if (READ_ERROR_EXPLANATIONS[code]) return READ_ERROR_EXPLANATIONS[code];
  const message = String(error.message ?? "").trim();
  return message || code.replace(/_/g, " ");
}

export function freshness(
  { observedAt, staleAfter, error, status, sourceName } = {},
  now = Date.now(),
) {
  const named = String(sourceName ?? "").trim();
  if (error || status === "error") {
    const code = String(error?.code ?? "").toLowerCase();
    const cause = READ_ERROR_LABELS[code]
      ? READ_ERROR_LABELS[code]
      : named
        ? `Can't reach ${named}`
        : "Can't reach source";
    // A read that failed just now does not erase a good read minutes ago;
    // say both while the last good read is still within its window.
    const observed = Date.parse(observedAt);
    const deadline = Date.parse(staleAfter);
    const stillFresh =
      Number.isFinite(observed) &&
      Number.isFinite(deadline) &&
      deadline > now &&
      observed <= now;
    return {
      key: "error",
      label: stillFresh ? `${cause} · last good read kept` : cause,
      tone: "warn",
      // The failed read did not cost the reader anything yet.
      kept: stillFresh,
    };
  }
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
  acknowledged: "Acknowledged failure",
  closed: "Closed, nothing delivered",
  source_reported: "Reported, not verified",
  sending: "Delivery in progress",
  unknown: "Delivery uncertain",
  superseded: "Replaced",
  declined: "Declined",
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
export function sentenceCase(text) {
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
      // A note names the field it sets: "Priority: high", not "High".
      if (String(item?.scope ?? "") === "work.annotate") {
        for (const [key, value] of Object.entries(structured)) {
          if (["string", "number", "boolean"].includes(typeof value)) {
            const text = String(value).trim();
            if (text) return `${key.replace(/_/g, " ")}: ${text}`;
          }
        }
      }
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
  return stripDecisionPrefix(instruction) || taskTitle || "Decision";
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
  const ref = String(work?.ref ?? "").trim();
  if (ref) return ref;
  const handle = String(work?.handle ?? "").trim();
  return handle;
}

/**
 * Board-column order for a phase: same board together, then membership rank.
 * work.list itself stays recency; board views sort after fetch.
 */
export function sortWorkBoardItems(items) {
  return [...(Array.isArray(items) ? items : [])].sort((a, b) => {
    const board = String(a?.board_ref ?? "").localeCompare(
      String(b?.board_ref ?? ""),
    );
    if (board) return board;
    const rank = String(a?.rank ?? "").localeCompare(String(b?.rank ?? ""));
    if (rank) return rank;
    return String(workKey(a) ?? "").localeCompare(String(workKey(b) ?? ""));
  });
}

/**
 * The revision a proposal must fence on. Core publishes it as
 * `decision_revision` (source.revision for a known external revision,
 * otherwise the Nexus work version); the fallback mirrors that rule for an
 * older core that omits the field.
 */
export function workTargetRevision(work) {
  const published = String(work?.decision_revision ?? "").trim();
  if (published) return published;
  const external =
    String(work?.source?.authority ?? "").toLowerCase() !== "nexus";
  const sourceRevision = String(work?.source?.revision ?? "").trim();
  if (external && sourceRevision) return sourceRevision;
  return String(work?.version ?? "0");
}

/**
 * Who proposed a decision, in the reader's words. `actorLabel` maps an actor
 * id to a display name; `currentActorId` turns the reader's own proposals
 * into "You".
 */
export function proposerLabel(
  item,
  { actorLabel = (id) => id, currentActorId = "" } = {},
) {
  const kind = String(item?.origin_kind ?? "").toLowerCase();
  const by = String(item?.proposed_by ?? "").trim();
  if (kind === "pm_turn") return "The PM proposes";
  if (kind === "channel") return "Proposed from a channel";
  if (by && currentActorId && by === currentActorId) return "You proposed";
  if (by) return `${actorLabel(by) || by} proposes`;
  return "Proposed";
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
  return work.ref || work.handle;
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
/** True when core answered 401 for a missing, invalid or expired token. */
export function isSessionExpired(error) {
  const status = Number(error?.status);
  const code = String(error?.body?.error?.code ?? "").toLowerCase();
  return (
    status === 401 ||
    code === "invalid_token" ||
    code === "auth_required" ||
    /token is invalid, expired, or revoked/i.test(String(error?.message ?? ""))
  );
}

export function errorMessage(error) {
  const raw =
    error instanceof Error
      ? error.message
      : String(error || "Unable to load workspace data.");
  if (
    /capacity reached|busy|already has an active turn|still queued or being answered/i.test(
      raw,
    ) ||
    ["conversation", "capacity", "queue"].includes(
      String(error?.body?.error?.details?.reason ?? ""),
    )
  ) {
    const details = error?.body?.error?.details ?? {};
    if (String(details.reason ?? "") === "queue") {
      // Released leases re-enter the queue, so "queued" can exceed the limit;
      // "21 of 20" reads as a bug rather than a full queue.
      const limit = details.limit ? ` (limit ${details.limit})` : "";
      return `The PM queue for this workspace is full${limit}. Your message is kept; send it again in a moment, once a waiting question is answered or expires.`;
    }
    if (String(details.reason ?? "") === "capacity") {
      const limit = details.limit
        ? ` (${details.in_flight ?? "?"} of ${details.limit})`
        : "";
      return `The PM is at its in-flight limit for this workspace${limit}. Your message is kept; send it once another turn finishes or expires, or release a stuck runner.`;
    }
    return "Your previous message in this conversation is still queued or being answered. Your draft stays in the composer; send it again once that turn finishes or expires, or copy it into a new conversation.";
  }
  if (isSessionExpired(error)) {
    return "Your session has expired. Sign in again to continue.";
  }
  if (/PM permission denied/i.test(raw)) {
    return "You are not signed in as someone who can use the PM. Sign in again and retry.";
  }
  // "anx-core request failed at same-origin: POST /x (400) - reason" — the
  // reason is the message; the transport prefix is log material.
  const transport = raw.match(
    /^anx-core request failed[^:]*:\s*[A-Z]+\s+\S+\s+\(\d{3}\)\s*-\s*(.+)$/s,
  );
  if (transport && transport[1].trim())
    return sentenceCase(transport[1].trim());
  return raw;
}

const DECISION_KIND_PREFIX =
  /^\s*(?:[A-Za-z][\w /-]{0,40}\s+)?decision\s*:\s*/i;

/**
 * The instruction without a "Producer decision:" style prefix. The PM writes
 * every proposal in the same voice, so the prefix is noise in a list where
 * every row would otherwise start with the same three words.
 */
export function stripDecisionPrefix(text) {
  return String(text ?? "")
    .trim()
    .replace(DECISION_KIND_PREFIX, "");
}

function firstSentence(text) {
  const s = String(text ?? "").trim();
  if (!s) return "";
  const match = s.match(/^.+?[.!?](?=\s|$)/);
  return (match ? match[0] : s).trim();
}

/**
 * Two lines for a decision: what it is about (the task) and what is being
 * asked (the first sentence of the proposal). Structured JSON instructions
 * fall back to the existing summary-field logic.
 */
const FIELD_LABELS = {
  next_action: "Next action",
  next_actor: "Next actor",
  owner: "Owner",
  accountable_owner: "Accountable owner",
  acceptance_criteria: "Acceptance criteria",
  due_at: "Due",
  priority: "Priority",
  summary: "Summary",
  title: "Title",
  question: "Question",
  reason: "Reason",
  message: "Message",
  action: "Action",
  instruction: "Instruction",
  blockers: "Blockers",
  phase: "Phase",
};

function fieldLabel(key) {
  if (FIELD_LABELS[key]) return FIELD_LABELS[key];
  return sentenceCase(String(key).replace(/[_-]+/g, " "));
}

/**
 * A structured instruction as readable rows: {label, value} for every scalar
 * or list field, in a stable order (known fields first). Empty for prose.
 */
export function decisionFields(item) {
  const structured = parseStructuredInstruction(item?.instruction);
  if (!structured || Array.isArray(structured)) return [];
  const known = Object.keys(FIELD_LABELS).filter((key) => key in structured);
  const rest = Object.keys(structured).filter((key) => !(key in FIELD_LABELS));
  const rows = [];
  for (const key of [...known, ...rest]) {
    const value = structured[key];
    if (value == null || value === "") continue;
    if (Array.isArray(value)) {
      const items = value
        .map((entry) => String(entry ?? "").trim())
        .filter(Boolean);
      if (items.length)
        rows.push({ label: fieldLabel(key), value: items.join("; ") });
    } else if (typeof value !== "object") {
      rows.push({ label: fieldLabel(key), value: String(value) });
    }
  }
  return rows;
}

export function decisionSummary(item, taskTitle = "") {
  const structured = parseStructuredInstruction(item?.instruction);
  if (structured) {
    const fields = decisionFields(item);
    const lead = fields.find((row) =>
      ["Next action", "Summary", "Question", "Action", "Instruction"].includes(
        row.label,
      ),
    );
    const ask = lead
      ? sentenceCase(
          lead.label === "Next action"
            ? `Sets next action to “${lead.value}”`
            : lead.value,
        )
      : "";
    const title = String(taskTitle ?? "").trim() || ask || "Proposed decision";
    return { title, ask: title === ask ? "" : ask };
  }
  const body = stripDecisionPrefix(item?.instruction);
  const ask = phaseAsk(item, body) || sentenceCase(firstSentence(body));
  const title = String(taskTitle ?? "").trim() || ask || "Decision";
  return { title, ask: title === ask ? "" : ask };
}

/**
 * A phase change says where it goes first: two "Request status change at
 * Nexus to…" rows truncated at the same width are indistinguishable.
 */
function phaseAsk(item, body) {
  if (String(item?.scope ?? "") !== "work.phase") return "";
  const phase = String(item?.payload?.phase ?? "").trim();
  if (!phase) return "";
  const at = body.match(/\bat\s+(.+?)\s+to\s+\S+/i)?.[1]?.trim() ?? "";
  return at ? `Move to ${label(phase)} at ${at}` : `Move to ${label(phase)}`;
}

/**
 * What approving will do, in the reader's words. Scope and ownership come
 * from core; nothing here changes what core enforces.
 */
export function decisionConsequence(item, work = null) {
  const scope = String(item?.scope ?? "");
  const source = work?.source ? sourceLabel(work.source) : "";
  const owned = work ? isNexusOwned(work) : false;
  const phase = String(item?.payload?.phase ?? "").trim();
  if (scope === "work.phase") {
    const target = phase ? ` to ${label(phase)}` : "";
    if (item?.deliverable === false) {
      const where = source && source !== "Nexus" ? source : "this source";
      return `No delivery path exists for ${where} yet. Approving records your decision${target} and keeps the request pending; nothing changes at ${where} until a delivery path exists.`;
    }
    if (!work)
      return `Approving authorizes this change${target}. If the task lives in another tracker, it is requested there rather than applied here.`;
    if (owned || source === "Nexus")
      return phase
        ? `Approving moves this task${target} in Nexus.`
        : "This proposal names no target phase, so it cannot be applied. Decline it and ask the PM to propose again.";
    return `Approving asks the PM to request this change${target} at ${source}. Nothing changes at ${source} until that request is delivered and read back.`;
  }
  if (scope === "work.annotate")
    return "Approving records the PM's note on this task in Nexus. The source is not changed.";
  if (["github", "multica", "ssh_git", "git"].includes(scope))
    return `Approving authorizes the PM to act at ${sourceLabel({ authority: scope })}. The result is read back before it counts as done.`;
  return "Approving authorizes exactly this proposal. Delivery and outcome are tracked below.";
}

/**
 * Core's messages carry RFC 3339 instants ("next attempt at 2026-09-13T19:34:49Z").
 * A reader wants "next attempt in 4m".
 */
export function humanizeInstants(text) {
  return String(text ?? "").replace(
    /(\bat )?(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))/g,
    (match, at, instant) => {
      const when = formatTimestamp(instant);
      if (!when || when === instant) return match;
      // "at in 4m" is not English; a relative time carries its own preposition.
      const relative = /^in \S|\bago$|^just now$|^in a moment$/.test(when);
      return relative || !at ? when : `${at}${when}`;
    },
  );
}
