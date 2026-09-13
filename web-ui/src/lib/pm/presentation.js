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

export function freshness(
  { observedAt, staleAfter, error, status, sourceName } = {},
  now = Date.now(),
) {
  const named = String(sourceName ?? "").trim();
  if (error || status === "error") {
    const code = String(error?.code ?? "").toLowerCase();
    const label = READ_ERROR_LABELS[code]
      ? READ_ERROR_LABELS[code]
      : named
        ? `Can't reach ${named}`
        : "Can't reach source";
    return { key: "error", label, tone: "warn" };
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
  if (/capacity reached|busy/i.test(raw)) {
    return "Your previous message in this conversation is still queued or being answered. Your new message is kept; send it once that turn finishes or expires, or start a new conversation.";
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
  const ask = sentenceCase(firstSentence(body));
  const title = String(taskTitle ?? "").trim() || ask || "Decision";
  return { title, ask: title === ask ? "" : ask };
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
    if (owned || !source || source === "Nexus")
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
