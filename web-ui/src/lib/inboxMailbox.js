import { getInboxSubjectLabel, enrichInboxItem } from "./inboxUtils.js";
import {
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

export function inboxItemNeedsResponse(item) {
  const status = String(item?.status ?? "").toLowerCase();
  if (status === "completed") return false;
  if (item?.completed_at || item?.responded_at) return false;
  return true;
}

export function classifyInboxRow(row, now = Date.now()) {
  if (row.kind === "decision") {
    if (row.status === "awaiting_answer") return "needs-you";
    if (WATCHING_DECISION_STATUSES.has(row.status)) return "watching";
    return "handled";
  }
  if (row.kind === "task") {
    if (row.phase === "blocked" || row.item?.phase === "blocked") {
      return "needs-you";
    }
    const freshness = workFreshness(row.item || row, now);
    if (freshness.key === "stale" || freshness.key === "error") {
      return "watching";
    }
    return "handled";
  }
  if (row.kind === "update") return "watching";
  if (row.kind === "inbox") {
    return inboxItemNeedsResponse(row.item) ? "needs-you" : "handled";
  }
  return "handled";
}

export function inboxRowBadge(row, now = Date.now()) {
  if (row.kind === "decision") {
    return receiptSignal(row.status);
  }
  if (row.kind === "task") {
    if (row.item?.phase === "blocked" || row.phase === "blocked") {
      return { label: "Blocked", tone: "warn", primary: true };
    }
    return workFreshness(row.item || row, now);
  }
  if (row.kind === "update") {
    return {
      label: row.count ? `${row.count}` : "Update",
      tone: "neutral",
      primary: true,
    };
  }
  return inboxItemNeedsResponse(row.item)
    ? { label: "Needs you", tone: "warn", primary: true }
    : { label: "Handled", tone: "neutral", primary: true };
}

export function buildInboxRows({
  decisions = [],
  work = [],
  inboxItems = [],
  updates = [],
  now = Date.now(),
} = {}) {
  const rows = [];
  for (const item of decisions) {
    rows.push({
      id: `decision:${item.id}`,
      kind: "decision",
      title: item.instruction || "Decision",
      source: item.work_ref || "",
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
      source: [sourceLabel(item.source), item.ref || workKey(item)]
        .filter(Boolean)
        .join(" · "),
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
      time: item.source_event_time || item.created_at || item.responded_at,
      status: item.status || (item.responded_at ? "completed" : "open"),
      item,
    });
  }
  for (const group of updates) {
    rows.push({
      id: `update:${group.group_ref || group.display_name}`,
      kind: "update",
      title: group.display_name || group.group_ref || "Update",
      source: group.group_type || "workspace",
      time: group.newest_event?.ts,
      status: "update",
      count: group.unread_count,
      grouped: true,
      item: group,
    });
  }
  return rows.map((row) => ({
    ...row,
    mailbox: classifyInboxRow(row, now),
  }));
}

export function filterMailbox(rows, mailbox) {
  return rows.filter((row) => row.mailbox === mailbox);
}
