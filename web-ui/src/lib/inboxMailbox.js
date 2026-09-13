import {
  enrichInboxItem,
  getInboxSubjectLabel,
  getInboxSubjectRef,
} from "./inboxUtils.js";
import {
  decisionSummary,
  receiptSignal,
  sourceLabel,
  workFreshness,
  workKey,
} from "./pm/presentation.js";

export const INBOX_MAILBOXES = [
  ["needs-you", "Needs you"],
  ["watching", "Watching"],
  ["handled", "Handled"],
];

// Decision status once the action receipt is joined in: an answered decision
// reads as its action's state until that state is final.
const WATCHING_DECISION_STATUSES = new Set([
  "answered",
  "pending",
  "pending_delivery",
  "delivered",
  "sending",
  "applied",
  "source_reported",
  "reported",
  "unknown",
]);

/**
 * The state a decision row should wear: the decision's own status until it is
 * answered, then its action's receipt status. A verified read-back is done; a
 * failed delivery needs the reader again.
 */
/**
 * An awaiting proposal can be gone (the task it names no longer exists),
 * moot (the task is already where it asks) or stale (the task changed
 * since). None can be approved; none is the reader's obligation. Core
 * publishes these as work_missing, already_at_target and target_current;
 * the work record is the fallback.
 */
export function proposalVoidReason(decision, work = null) {
  if (String(decision?.status ?? "") !== "awaiting_answer") return "";
  if (decision?.work_missing === true) return "gone";
  if (decision?.already_at_target === true) return "moot";
  if (decision?.target_current === false) return "stale";
  if (!work) return "";
  const phase = String(decision?.payload?.phase ?? "").trim();
  if (phase && phase === String(work.phase ?? "")) return "moot";
  const target = String(decision?.target_revision ?? "").trim();
  const current = String(work.decision_revision ?? "").trim();
  if (target && current && target !== current) return "stale";
  return "";
}

export function decisionRowStatus(
  decision,
  actions = [],
  { receiptsUnavailable = false, work = null } = {},
) {
  const own = String(decision?.status ?? "");
  const voidReason = proposalVoidReason(decision, work);
  if (voidReason) return `void_${voidReason}`;
  // A decline used to be stored as superseded with no replacement.
  if (own === "superseded" && !decision?.superseded_by) return "declined";
  if (own !== "answered") return own;
  // Receipts could not be loaded: the delivery state is unknown to us, and
  // an unknown delivery belongs in front of the reader, not under Watching.
  if (receiptsUnavailable) return "receipt_unavailable";
  const action = actions.find(
    (item) =>
      item &&
      ((decision.action_id && item.id === decision.action_id) ||
        item.decision_id === decision.id),
  );
  if (!action) return own;
  const status = String(action.status ?? "");
  if (status === "verified" && action.receipt?.independently_verified !== true)
    return "source_reported";
  // A closed request that never left core is not a failure.
  if (
    status === "acknowledged" &&
    (action.closed_without_delivery === true ||
      (action.deliverable === false &&
        !(action.attempts || []).some((attempt) => attempt?.sent_at)))
  )
    return "closed";
  return status || own;
}

const LOUD_SEVERITIES = new Map([
  ["critical", { label: "Critical", tone: "danger" }],
  ["high", { label: "High", tone: "warn" }],
]);

export function inboxItemNeedsResponse(item) {
  const status = String(item?.status ?? "").toLowerCase();
  if (status === "completed") return false;
  if (item?.completed_at || item?.responded_at) return false;
  return true;
}

function taskIsBlocked(row) {
  return row?.phase === "blocked" || row?.item?.phase === "blocked";
}

/**
 * Which mailbox a row belongs to, or `null` when it does not belong in the
 * Inbox at all.
 *
 * A task enters the Inbox only when it is blocked (Needs you) or when its
 * evidence went stale or its source could not be reached (Watching). An
 * untouched task is not inbox work: counting those under Handled made the
 * Handled tab a second, worse copy of the task list and made "Handled" mean
 * "we never looked at it".
 */
export function classifyInboxRow(row, now = Date.now()) {
  if (row.kind === "decision") {
    // A decision addressed to someone else is not this reader's work; it is
    // visible under Watching so the workspace stays legible, never under
    // Needs you.
    if (row.status === "awaiting_answer")
      return row.item?.can_answer === false ? "watching" : "needs-you";
    // A failed delivery is the reader's problem again, not a thing to watch.
    if (row.status === "failed" || row.status === "receipt_unavailable")
      return "needs-you";
    // A moot, stale or orphaned proposal is nobody's obligation; it waits to
    // be tidied.
    if (
      row.status === "void_moot" ||
      row.status === "void_stale" ||
      row.status === "void_gone"
    )
      return "watching";
    if (WATCHING_DECISION_STATUSES.has(row.status)) return "watching";
    return "handled";
  }
  if (row.kind === "task") {
    if (taskIsBlocked(row)) {
      return "needs-you";
    }
    const freshness = workFreshness(row.item || row, now);
    if (freshness.key === "stale" || freshness.key === "error") {
      return "watching";
    }
    return null;
  }
  if (row.kind === "update") return "watching";
  if (row.kind === "inbox") {
    return inboxItemNeedsResponse(row.item) ? "needs-you" : "handled";
  }
  return "handled";
}

/**
 * The badge for a row, or `null` when a badge would say nothing.
 *
 * A badge earns its place only by carrying information the two text lines do
 * not: that a task is blocked, that an ask is loud, that a source cannot be
 * reached, where a decision's receipt got to. "Needs you" inside Needs you and
 * "Handled" inside Handled are not information.
 */
export function inboxRowBadge(row, now = Date.now()) {
  if (row.kind === "task") {
    if (taskIsBlocked(row)) {
      return { label: "Blocked", tone: "warn" };
    }
    const freshness = workFreshness(row.item || row, now);
    // The label names the cause (reader not ready, rate limited, unreachable),
    // the same way the task page does.
    if (freshness.key === "error")
      return { label: freshness.label, tone: "warn" };
    return null;
  }
  if (row.kind === "decision") {
    // Awaiting answer only ever shows inside Needs you, where the badge would
    // repeat the mailbox back at the reader — unless it is someone else's.
    if (row.status === "awaiting_answer")
      return row.item?.can_answer === false
        ? { label: "Waiting on someone else", tone: "neutral" }
        : null;
    if (row.status === "superseded" && row.item?.superseded_by)
      return { label: "Replaced", tone: "neutral" };
    if (row.status === "declined")
      return { label: "Declined", tone: "neutral" };
    if (row.status === "receipt_unavailable")
      return { label: "Delivery state unknown", tone: "warn" };
    if (row.status === "void_moot")
      return { label: "Already there", tone: "neutral" };
    if (row.status === "void_stale")
      return { label: "Task changed since", tone: "neutral" };
    if (row.status === "void_gone")
      return { label: "Task no longer exists", tone: "neutral" };
    return receiptSignal(row.status);
  }
  if (row.kind === "update") {
    return row.count > 1 ? { label: String(row.count), tone: "neutral" } : null;
  }
  if (row.kind === "inbox") {
    return (
      LOUD_SEVERITIES.get(String(row.severity ?? "").toLowerCase()) ?? null
    );
  }
  return null;
}

export function buildInboxRows({
  decisions = [],
  actions = [],
  receiptsUnavailable = false,
  work = [],
  inboxItems = [],
  updates = [],
  now = Date.now(),
} = {}) {
  const rows = [];
  const taskTitles = new Map();
  const workByRef = new Map();
  for (const item of work) {
    if (item && workKey(item)) workByRef.set(workKey(item), item);
    if (!item || !workKey(item)) continue;
    const title = String(item.title || "").trim();
    taskTitles.set(workKey(item), title);
    // Inbox items may name a card by id rather than public ref.
    if (item.id) taskTitles.set(`card:${item.id}`, title);
    if (item.handle) taskTitles.set(`card:${item.handle}`, title);
  }
  for (const item of decisions) {
    const summary = decisionSummary(item, taskTitles.get(item.work_ref) || "");
    rows.push({
      id: `decision:${item.id}`,
      kind: "decision",
      // The task is what the row is about; the ask is the second line. The
      // raw work ref is pane-header material, never a list line.
      title: summary.title,
      // A trashed task has no title to lead with; the ask leads and the
      // second line says why the subject is gone.
      source:
        item.work_missing === true && !taskTitles.get(item.work_ref)
          ? "Task no longer exists"
          : summary.ask || "Decision",
      ref: item.work_ref || "",
      time: item.updated_at || item.created_at,
      status: decisionRowStatus(item, actions, {
        receiptsUnavailable,
        work: workByRef.get(item.work_ref) || null,
      }),
      phase: item.status,
      item,
    });
  }
  for (const item of work) {
    rows.push({
      id: `task:${workKey(item)}`,
      kind: "task",
      title: item.title || "Untitled task",
      source: sourceLabel(item.source),
      ref: item.ref || workKey(item),
      time: item.freshness?.last_observed_at || item.updated_at,
      status: item.phase,
      phase: item.phase,
      item,
    });
  }
  for (const raw of inboxItems) {
    const item = enrichInboxItem(raw);
    const subjectRef = String(getInboxSubjectRef(item) ?? "").trim();
    const subjectTitle = taskTitles.get(subjectRef);
    rows.push({
      id: `inbox:${item.id}`,
      kind: "inbox",
      title: item.title || item.summary || "Inbox item",
      // Name the subject when we know it; a raw ref is a last resort.
      source: subjectTitle
        ? `Task: ${subjectTitle}`
        : getInboxSubjectLabel(item) || "",
      ref: item.subject_ref || "",
      time: item.source_event_time || item.created_at || item.responded_at,
      status: item.status || (item.responded_at ? "completed" : "open"),
      category: String(item.kind ?? item.category ?? "").trim(),
      severity: item.severity || "",
      requesterLabel:
        String(item.requester_label ?? "").trim() ||
        String(item.requester_agent_id ?? "").trim() ||
        String(item.requester_actor_id ?? "").trim(),
      body: item.body || "",
      responseProposals: Array.isArray(item.response_proposals)
        ? item.response_proposals
            .map((value) => String(value ?? "").trim())
            .filter(Boolean)
        : [],
      item,
    });
  }
  for (const group of updates) {
    rows.push({
      id: `update:${group.group_ref || group.display_name}`,
      kind: "update",
      title: group.display_name || group.group_ref || "Update",
      source: updateGroupSource(group.group_type),
      ref: group.group_ref || "",
      time: group.newest_event?.ts,
      status: "update",
      count: group.unread_count,
      grouped: true,
      item: group,
    });
  }
  return rows
    .map((row) => ({ ...row, mailbox: classifyInboxRow(row, now) }))
    .filter((row) => row.mailbox !== null)
    .sort((a, b) => rowTime(b) - rowTime(a));
}

// Newest first across kinds; a row without a time sorts last, in input order.
function rowTime(row) {
  const t = Date.parse(row?.time ?? "");
  return Number.isFinite(t) ? t : Number.NEGATIVE_INFINITY;
}

const UPDATE_GROUP_SOURCES = {
  board: "Board updates",
  topic: "Topic updates",
  thread: "Thread updates",
  workspace: "Workspace updates",
};
function updateGroupSource(type) {
  const key = String(type ?? "").toLowerCase();
  return UPDATE_GROUP_SOURCES[key] || (key ? `${key} updates` : "Updates");
}

export function filterMailbox(rows, mailbox) {
  return rows.filter((row) => row.mailbox === mailbox);
}
