import { getInboxSubjectLabel, enrichInboxItem } from "./inboxUtils.js";
import {
  decisionTitle,
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

const WATCHING_DECISION_STATUSES = new Set([
  "answered",
  "pending_delivery",
  "delivered",
  "sending",
  "acknowledged",
  "applied",
  "source_reported",
  "failed",
]);

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
    if (row.status === "awaiting_answer") return "needs-you";
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
    if (freshness.key === "error") {
      const name = sourceLabel(row.item?.source);
      return {
        label: name ? `Can't reach ${name}` : "Can't reach source",
        tone: "warn",
      };
    }
    return null;
  }
  if (row.kind === "decision") {
    // Awaiting answer only ever shows inside Needs you, where the badge would
    // repeat the mailbox back at the reader.
    if (row.status === "awaiting_answer") return null;
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
  work = [],
  inboxItems = [],
  updates = [],
  now = Date.now(),
} = {}) {
  const rows = [];
  const taskTitles = new Map(
    work
      .filter((item) => item && workKey(item))
      .map((item) => [workKey(item), String(item.title || "").trim()]),
  );
  for (const item of decisions) {
    rows.push({
      id: `decision:${item.id}`,
      kind: "decision",
      title: decisionTitle(item, taskTitles.get(item.work_ref) || ""),
      // The raw work ref is pane-header material, never a list line.
      source: taskTitles.get(item.work_ref) || "Decision",
      ref: item.work_ref || "",
      time: item.updated_at || item.created_at,
      status: item.status,
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
    rows.push({
      id: `inbox:${item.id}`,
      kind: "inbox",
      title: item.title || item.summary || "Inbox item",
      source: getInboxSubjectLabel(item) || "",
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
      source: group.group_type || "workspace",
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
    .filter((row) => row.mailbox !== null);
}

export function filterMailbox(rows, mailbox) {
  return rows.filter((row) => row.mailbox === mailbox);
}
