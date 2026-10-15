/**
 * `brief.v1`: the one read Decide, Watch, the initiative page and
 * `anx orient --json` are built from (end-state §2.5, invariant I7).
 *
 * Everything a human sees about phase, health, progress or a promise is a
 * brief field, rendered with `data-source="computed:<field>"` so the I1 lint
 * can check it (`BRIEF_COMPUTED_FIELDS`). Nothing here invents state: the
 * helpers sort, group and format what core computed.
 *
 * All helpers are pure and take `now` so goldens are deterministic.
 */

import { formatAgo } from "$lib/time/format.js";

/** The four computed initiative states, in attention order. */
export const WATCH_STATES = ["past_promise", "moving", "no_promise", "done"];

export const WATCH_STATE_LABELS = {
  past_promise: "Past promise",
  moving: "Moving",
  no_promise: "No promise",
  done: "Done",
};

/** Board columns: placement, which humans and agents set. Not a state. */
export const PLACEMENTS = ["backlog", "ready", "in_progress", "review", "done"];

export const PLACEMENT_LABELS = {
  backlog: "Backlog",
  ready: "Ready",
  in_progress: "In progress",
  review: "Review",
  done: "Done",
};

export const DECISION_KIND_LABELS = {
  ask: "Ask",
  review: "Review",
  access: "Access",
  host: "Host",
  pm_proposal: "PM proposal",
};

/**
 * Brief fields a screen may bind phase, health, progress or promise to.
 * `data-source="computed:<field>"` must name one of these (I1).
 */
export const BRIEF_COMPUTED_FIELDS = new Set([
  "decisions.decide_by",
  "decisions.overdue_for",
  "decisions.blocks",
  "decisions.priority",
  "initiatives.state",
  "initiatives.placement",
  "initiatives.promise",
  "initiatives.past_promise_for",
  "initiatives.promise_moves",
  "initiatives.last_evidence_at",
  "initiatives.progress",
  "initiatives.blocked_count",
  "initiatives.waiting_on_you_count",
  "generated_at",
]);

const PRIORITY_RANK = {
  critical: 0,
  p0: 0,
  high: 1,
  p1: 1,
  medium: 2,
  p2: 2,
  low: 3,
  p3: 3,
};

function text(value) {
  return String(value ?? "").trim();
}

function time(value) {
  const ms = Date.parse(text(value));
  return Number.isFinite(ms) ? ms : null;
}

function seconds(record, field) {
  const direct = Number(record?.[`${field}_seconds`] ?? record?.[field]);
  return Number.isFinite(direct) && direct > 0 ? direct : 0;
}

function actor(value) {
  if (!value) return null;
  if (typeof value === "string") return { id: value, display: value };
  const id = text(value.id);
  const display = text(value.display) || id;
  const kind = text(value.kind);
  return id || display ? { id, display, ...(kind ? { kind } : {}) } : null;
}

function refTitle(value) {
  if (!value) return null;
  if (typeof value === "string") return { ref: value, title: "" };
  const ref = text(value.ref);
  return ref ? { ref, title: text(value.title) } : null;
}

function normalizeDecision(raw) {
  const ref = text(raw?.ref);
  if (!ref) return null;
  const kind = text(raw.kind) || "ask";
  return {
    ref,
    kind,
    inboxItemId: text(raw.inbox_item_id) || text(raw.id) || ref,
    workspace: text(raw.workspace),
    title: text(raw.title) || ref,
    body: String(raw.body ?? ""),
    requestedBy: actor(raw.requested_by),
    createdAt: text(raw.created_at),
    decideBy: text(raw.decide_by),
    overdueFor: seconds(raw, "overdue_for"),
    blocks: (Array.isArray(raw.blocks) ? raw.blocks : [])
      .map(refTitle)
      .filter(Boolean),
    // Exact count, including refs this reader cannot see (never their titles).
    blocksCount: Math.max(
      Number(raw.blocks_count) || 0,
      Array.isArray(raw.blocks) ? raw.blocks.length : 0,
    ),
    priority: text(raw.priority).toLowerCase(),
    subject: refTitle(raw.subject),
    // A PM proposal answers through /pm/decisions, which needs its revision.
    pmDecision: raw.pm_decision?.id
      ? {
          id: text(raw.pm_decision.id),
          revision: Number(raw.pm_decision.revision) || 0,
        }
      : null,
    // A host enrollment answers on the Access page's own routes.
    accessRequestId: text(raw.access_request_id),
    curation: normalizeCuration(raw.curation, refTitle(raw.subject)),
    batch: normalizeBatch(raw.batch),
    // Core orders proposals with the recommendation first; keep that order.
    proposals: decisionProposals(raw),
  };
}

/**
 * A batch: several open asks shown as one decision. Asks filed together
 * through POST /asks/batch carry a `filed:` key and cannot be ungrouped; an
 * accepted curation batch can.
 */
function normalizeBatch(raw) {
  if (!raw || typeof raw !== "object") return null;
  const key = text(raw.key);
  if (!key) return null;
  const members = (Array.isArray(raw.members) ? raw.members : [])
    .filter((member) => member && typeof member === "object")
    .map((member) => ({
      ref: text(member.ref),
      inboxItemId: text(member.inbox_item_id),
      title: text(member.title),
      subjectRef: text(member.subject_ref),
      blocks: (Array.isArray(member.blocks) ? member.blocks : [])
        .map(refTitle)
        .filter(Boolean),
    }))
    .filter((member) => member.ref || member.title);
  return {
    key,
    ungroupable: !key.startsWith("filed:"),
    count: Math.max(Number(raw.count) || 0, members.length),
    members,
  };
}

/** A curation proposal: what the PM wants to do, to which refs, and why. */
function normalizeCuration(raw, subject) {
  if (!raw || typeof raw !== "object") return null;
  // Targets may be bare refs; the subject's title is the only one core has
  // already cleared for this reader, so only it fills a missing title.
  const targets = (Array.isArray(raw.targets) ? raw.targets : [])
    .map(refTitle)
    .filter(Boolean)
    .map((target) =>
      !target.title && subject?.ref === target.ref && subject.title
        ? { ...target, title: subject.title }
        : target,
    );
  return {
    action: text(raw.action).replace(/[-_]+/g, " "),
    targets,
    source: text(raw.source),
  };
}

/**
 * The asker's proposals, recommended first. A curation proposal always
 * answers in one tap: proposals[0] accepts, and Dismiss is offered when the
 * asker gave no alternative.
 */
function decisionProposals(raw) {
  const proposals = (Array.isArray(raw.proposals) ? raw.proposals : [])
    .map(text)
    .filter(Boolean);
  if (!raw.curation || typeof raw.curation !== "object") return proposals;
  if (proposals.length === 0) proposals.push("Accept");
  if (proposals.length === 1) proposals.push("Dismiss");
  return proposals;
}

function normalizeInitiative(raw) {
  const ref = text(raw?.ref);
  if (!ref) return null;
  const state = WATCH_STATES.includes(text(raw.state))
    ? text(raw.state)
    : "no_promise";
  const placement = PLACEMENTS.includes(text(raw.placement))
    ? text(raw.placement)
    : state === "done"
      ? "done"
      : "backlog";
  const promise = raw.promise
    ? {
        at: text(raw.promise.at),
        what: text(raw.promise.what),
        source: text(raw.promise.source),
      }
    : null;
  const progress =
    raw.progress && Number(raw.progress.total) > 0
      ? {
          done: Math.max(0, Number(raw.progress.done) || 0),
          total: Number(raw.progress.total),
        }
      : null;
  const note = raw.note?.text
    ? {
        text: String(raw.note.text),
        author: actor(raw.note.author),
        at: text(raw.note.at),
      }
    : null;
  return {
    ref,
    id: ref.replace(/^card:/, ""),
    title: text(raw.title) || ref,
    state,
    placement,
    promise,
    pastPromiseFor: seconds(raw, "past_promise_for"),
    promiseMoves: Math.max(0, Number(raw.promise_moves) || 0),
    lastEvidenceAt: text(raw.last_evidence_at),
    progress,
    blockedCount: Math.max(0, Number(raw.blocked_count) || 0),
    waitingOnYouCount: Math.max(0, Number(raw.waiting_on_you_count) || 0),
    note,
    possiblyMoot: raw.possibly_moot?.flagged
      ? { reason: text(raw.possibly_moot.reason) }
      : null,
  };
}

function normalizeAnswered(raw) {
  const ref = text(raw?.ref);
  if (!ref) return null;
  return {
    ref,
    inboxItemId: text(raw.inbox_item_id) || ref,
    title: text(raw.title) || ref,
    answer: String(raw.answer ?? ""),
    outcome: text(raw.outcome),
    answeredAt: text(raw.answered_at),
    answeredBy: actor(raw.answered_by),
    followedRecommendation: Boolean(raw.followed_recommendation),
  };
}

/** Normalize a raw `brief.v1` response. Unknown fields are ignored, never fatal. */
export function normalizeBrief(raw) {
  const brief = raw;
  return {
    schema: text(brief?.schema) || "brief.v1",
    workspace: text(brief?.workspace),
    principal: actor(brief?.principal),
    // Core does not know its hosted slug; the shell supplies `workspace`.
    asOf: text(brief?.generated_at),
    truncated: {
      decisions: Boolean(brief?.truncated?.decisions),
      initiatives: Boolean(brief?.truncated?.initiatives),
    },
    decisions: (Array.isArray(brief?.decisions) ? brief.decisions : [])
      .map(normalizeDecision)
      .filter(Boolean),
    initiatives: (Array.isArray(brief?.initiatives) ? brief.initiatives : [])
      .map(normalizeInitiative)
      .filter(Boolean),
    yourAsks: (Array.isArray(brief?.your_asks) ? brief.your_asks : [])
      .map(normalizeDecision)
      .filter(Boolean),
    answeredForYou: (Array.isArray(brief?.answered_for_you)
      ? brief.answered_for_you
      : []
    )
      .map(normalizeAnswered)
      .filter(Boolean),
  };
}

/**
 * Decide order: decide_by (earliest first, none last), then blocks count
 * (most first), then priority, then age (oldest first), then ref.
 */
export function compareDecisions(a, b) {
  const aBy = time(a.decideBy);
  const bBy = time(b.decideBy);
  if (aBy !== bBy) {
    if (aBy === null) return 1;
    if (bBy === null) return -1;
    return aBy - bBy;
  }
  if (a.blocksCount !== b.blocksCount) return b.blocksCount - a.blocksCount;
  const aP = PRIORITY_RANK[a.priority] ?? 9;
  const bP = PRIORITY_RANK[b.priority] ?? 9;
  if (aP !== bP) return aP - bP;
  const aAt = time(a.createdAt) ?? Infinity;
  const bAt = time(b.createdAt) ?? Infinity;
  if (aAt !== bAt) return aAt - bAt;
  return a.ref.localeCompare(b.ref);
}

export function sortDecisions(decisions) {
  return [...(decisions ?? [])].sort(compareDecisions);
}

/** Watch list order: state, then worst first inside each state. */
export function compareInitiatives(a, b) {
  const s = WATCH_STATES.indexOf(a.state) - WATCH_STATES.indexOf(b.state);
  if (s) return s;
  if (a.state === "past_promise" && a.pastPromiseFor !== b.pastPromiseFor)
    return b.pastPromiseFor - a.pastPromiseFor;
  if (a.state === "moving") {
    const aAt = time(a.promise?.at) ?? Infinity;
    const bAt = time(b.promise?.at) ?? Infinity;
    if (aAt !== bAt) return aAt - bAt;
  }
  if (a.state === "no_promise" || a.state === "done") {
    const aEv = time(a.lastEvidenceAt) ?? 0;
    const bEv = time(b.lastEvidenceAt) ?? 0;
    if (a.state === "done" && aEv !== bEv) return bEv - aEv;
    if (a.state === "no_promise" && aEv !== bEv) return aEv - bEv;
  }
  return a.title.localeCompare(b.title);
}

/** `[{ state, label, rows }]` in attention order; empty groups dropped. */
export function groupWatch(initiatives) {
  const sorted = [...(initiatives ?? [])].sort(compareInitiatives);
  return WATCH_STATES.map((state) => ({
    state,
    label: WATCH_STATE_LABELS[state],
    rows: sorted.filter((row) => row.state === state),
  })).filter((group) => group.rows.length);
}

/** `[{ placement, label, rows }]` for every board column, empty or not. */
export function groupBoard(initiatives) {
  const sorted = [...(initiatives ?? [])].sort(compareInitiatives);
  return PLACEMENTS.map((placement) => ({
    placement,
    label: PLACEMENT_LABELS[placement],
    rows: sorted.filter((row) => row.placement === placement),
  }));
}

const DAY = 86_400;
const HOUR = 3_600;

/** "9d", "3h", "12m", "now". Exact, never "6+". */
export function formatSpan(totalSeconds) {
  const s = Math.max(0, Math.floor(Number(totalSeconds) || 0));
  if (s >= DAY) return `${Math.floor(s / DAY)}d`;
  if (s >= HOUR) return `${Math.floor(s / HOUR)}h`;
  if (s >= 60) return `${Math.floor(s / 60)}m`;
  return "now";
}

export function ageSeconds(iso, now = Date.now()) {
  const at = time(iso);
  return at === null ? null : Math.max(0, Math.floor((now - at) / 1000));
}

const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
];

/** "Oct 21" in UTC, so goldens do not depend on the machine's zone. */
export function formatDay(iso) {
  const at = time(iso);
  if (at === null) return "";
  const date = new Date(at);
  return `${MONTHS[date.getUTCMonth()]} ${date.getUTCDate()}`;
}

/**
 * Authored one-liners (a promise's `what`) are markdown. The computed line is
 * plain text, so strip the inline markup rather than print `**fix**`. Kept
 * dependency-free: this module loads with the shell, the markdown renderer
 * does not.
 */
export function plainInline(value) {
  return text(value)
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/(\*\*|__)(?=\S)([\s\S]*?\S)\1/g, "$2")
    .replace(/(^|[^\w*])([*_])(?=\S)([^*_]*?\S)\2(?![\w*])/g, "$1$3")
    .replace(/~~(?=\S)([\s\S]*?\S)~~/g, "$1")
    .replace(/^\s{0,3}(?:#{1,6}\s+|>\s?|[-*+]\s+(?:\[[ xX]\]\s+)?)/, "")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * The computed line under an initiative's title. Each part names the brief
 * field it came from, so the renderer can tag it for the I1 lint.
 *
 * @returns {{ field: string, text: string }[]}
 */
export function initiativeLine(row, now = Date.now()) {
  const parts = [];
  if (row.state === "past_promise") {
    if (row.promise?.at)
      parts.push({
        field: "initiatives.promise",
        text: `promised ${formatDay(row.promise.at)}`,
      });
    parts.push({
      field: "initiatives.past_promise_for",
      text: `${formatSpan(row.pastPromiseFor)} past`,
    });
  } else if (row.state === "moving") {
    const age = ageSeconds(row.lastEvidenceAt, now);
    if (age !== null)
      parts.push({
        field: "initiatives.last_evidence_at",
        text: `evidence ${formatAgo(row.lastEvidenceAt, now)}`,
      });
    if (row.promise)
      parts.push({
        field: "initiatives.promise",
        text: row.promise.what
          ? `next: ${plainInline(row.promise.what)}${row.promise.at ? ` · ${formatDay(row.promise.at)}` : ""}`
          : `next ${formatDay(row.promise.at)}`,
      });
  } else if (row.state === "no_promise") {
    const age = ageSeconds(row.lastEvidenceAt, now);
    parts.push({
      field: "initiatives.last_evidence_at",
      text:
        age === null
          ? "no evidence yet"
          : `last evidence ${formatAgo(row.lastEvidenceAt, now)}`,
    });
  } else if (row.state === "done") {
    const age = ageSeconds(row.lastEvidenceAt, now);
    if (age !== null)
      parts.push({
        field: "initiatives.last_evidence_at",
        text: `done ${formatAgo(row.lastEvidenceAt, now)}`,
      });
  }
  if (row.promiseMoves > 0 && row.state !== "done")
    parts.push({
      field: "initiatives.promise_moves",
      text: `moved ${row.promiseMoves}×`,
    });
  return parts;
}

/** "by Fri"-style deadline copy for a decision; overdue says so. */
export function decisionDeadline(decision, now = Date.now()) {
  if (decision.overdueFor > 0)
    return {
      field: "decisions.overdue_for",
      text: `${formatSpan(decision.overdueFor)} overdue`,
      overdue: true,
    };
  if (!decision.decideBy) return null;
  const at = time(decision.decideBy);
  if (at !== null && at < now)
    return {
      field: "decisions.decide_by",
      text: `${formatSpan((now - at) / 1000)} overdue`,
      overdue: true,
    };
  return {
    field: "decisions.decide_by",
    text: `by ${formatDay(decision.decideBy)}`,
    overdue: false,
  };
}

/** "as of <relative time>"; empty when the brief carries no time. */
export function freshnessLabel(asOf, now = Date.now()) {
  const label = formatAgo(asOf, now);
  return label ? `as of ${label}` : "";
}

/** True when this decision, or any ask grouped into it, names the initiative. */
function decisionTouchesInitiative(decision, wanted) {
  if (decision.subject?.ref === wanted) return true;
  if (decision.blocks?.some((block) => block.ref === wanted)) return true;
  return (decision.batch?.members ?? []).some(
    (member) =>
      member.subjectRef === wanted ||
      (member.blocks ?? []).some((block) => block.ref === wanted),
  );
}

/** Decisions whose subject or blocks name this initiative, including any grouped ask. */
export function decisionsForInitiative(brief, ref) {
  const wanted = text(ref);
  if (!wanted) return [];
  return sortDecisions(
    (brief?.decisions ?? []).filter((decision) =>
      decisionTouchesInitiative(decision, wanted),
    ),
  );
}

function decisionMatchesId(decision, wanted) {
  if (decision.ref === wanted || decision.inboxItemId === wanted) return true;
  return (decision.batch?.members ?? []).some(
    (member) => member.ref === wanted || member.inboxItemId === wanted,
  );
}

/**
 * Find one decision by its ref or inbox item id, or by a grouped member's
 * ref or inbox item id. A member permalink opens the group.
 */
export function findDecision(brief, id) {
  const wanted = text(id);
  if (!wanted) return null;
  return (
    (brief?.decisions ?? []).find((decision) =>
      decisionMatchesId(decision, wanted),
    ) ?? null
  );
}

function overlayCoversDecision(decision, overlay) {
  if (!decision || !overlay) return false;
  if (
    (decision.inboxItemId && overlay[decision.inboxItemId]) ||
    (decision.ref && overlay[decision.ref])
  ) {
    return true;
  }
  return (decision.batch?.members ?? []).some(
    (member) =>
      (member.inboxItemId && overlay[member.inboxItemId]) ||
      (member.ref && overlay[member.ref]),
  );
}

/**
 * How many decisions still wait. An answer recorded against the batch id, or
 * against any member, drops the whole group.
 */
export function countWaitingDecisions(decisions, overlay = {}) {
  return (decisions ?? []).filter(
    (decision) => !overlayCoversDecision(decision, overlay),
  ).length;
}
