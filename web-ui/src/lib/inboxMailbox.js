import { accessRequestFromInboxItem } from "$lib/accessGrant.js";
import { isHumanNextActor } from "./humanActors.js";
import { updateDigest } from "./inboxDigest.js";
import {
  enrichInboxItem,
  getInboxSubjectRef,
  inboxItemMailboxId,
  inboxSubjectNoun,
  splitTypedRef,
} from "./inboxUtils.js";
import {
  decisionSummary,
  label as phaseLabel,
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
  { receiptsUnavailable = false, work = null, currentActorId = "" } = {},
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
  // An approval that has a delivery path and was never sent still needs the
  // approver's click; only the approver, and only while it is deliverable.
  if (
    (status === "pending_delivery" || status === "pending") &&
    action.deliverable !== false &&
    !(action.attempts || []).some((attempt) => attempt?.sent_at) &&
    currentActorId &&
    String(decision?.actor_id ?? "") === currentActorId
  )
    return "awaiting_delivery";
  // A closed request that never left core is not a failure; one that failed
  // before it could be sent is, and its acknowledgement says so.
  const attempts = action.attempts || [];
  if (
    status === "acknowledged" &&
    !attempts.some((attempt) => attempt?.status === "failed") &&
    (action.closed_without_delivery === true ||
      (action.deliverable === false &&
        !attempts.some((attempt) => attempt?.sent_at)))
  )
    return "closed";
  return status || own;
}

const LOUD_SEVERITIES = new Map([
  ["critical", { label: "Critical", tone: "danger" }],
  ["high", { label: "High", tone: "warn" }],
]);

/**
 * A report review reminder: core telling an author that a hand-written panel
 * has passed its review date.
 *
 * Nobody answers it. It carries no response proposals, and core drops it from
 * every inbox read as soon as the report is revised, archived, trashed or
 * unpinned — so refreshing the panel is what closes it, and there is nothing
 * for a reply or an acknowledgement to do.
 */
export function inboxItemIsReminder(item) {
  return String(item?.kind ?? "").toLowerCase() === "report_review";
}

/** Whether core has closed this item, however it was closed. */
function inboxItemIsDone(item) {
  if (String(item?.status ?? "").toLowerCase() === "completed") return true;
  return Boolean(item?.completed_at || item?.responded_at);
}

export function inboxItemNeedsResponse(item) {
  if (inboxItemIsDone(item)) return false;
  // Offering Reply and Acknowledge on a reminder offered two buttons that
  // fail: there is no requester waiting and nothing to acknowledge to.
  if (inboxItemIsReminder(item)) return false;
  return true;
}

function taskIsBlocked(row) {
  const computed = row?.item?.work_summary?.status?.state;
  if (computed) return computed === "blocked";
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
    // A failed delivery is the reader's problem again, not a thing to watch;
    // so is an approval the reader has yet to deliver.
    if (
      row.status === "failed" ||
      row.status === "receipt_unavailable" ||
      row.status === "awaiting_delivery"
    )
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
    if (taskIsBlocked(row) || row.humanNext) {
      return "needs-you";
    }
    const freshness = workFreshness(row.item || row, now);
    if (
      row.item?.work_summary?.status?.state === "stale" ||
      freshness.key === "stale" ||
      freshness.key === "error"
    ) {
      return "watching";
    }
    return null;
  }
  if (row.kind === "update") return "watching";
  if (row.kind === "inbox") {
    // A reminder needs no response and still belongs in front of its author:
    // the panel it names is theirs to refresh. Filing it under Handled would
    // mean "we never looked at it".
    //
    // Core does not close these — a reminder simply stops being returned once
    // the report is revised, archived, trashed or unpinned. The closed arm is
    // there so a closed one would not sit in Needs you for ever if that
    // changes.
    if (inboxItemIsReminder(row.item))
      return inboxItemIsDone(row.item) ? "handled" : "needs-you";
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
  /*
   * A send that failed outranks everything else a row could say. The item is
   * still unanswered and still here; without this the row came back from an
   * optimistic removal looking exactly like one nobody had touched.
   */
  if (row.responseError) return { label: "Not sent", tone: "danger" };
  if (row.kind === "task") {
    const status = row.item?.work_summary?.status;
    if (
      status?.label &&
      ["blocked", "stale", "at_risk"].includes(status.state)
    ) {
      return { label: status.label, tone: "warn" };
    }
    if (taskIsBlocked(row)) return { label: "Blocked", tone: "warn" };
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
    if (row.status === "awaiting_delivery")
      return { label: "Deliver", tone: "warn" };
    if (row.status === "void_moot")
      return { label: "Already there", tone: "neutral" };
    if (row.status === "void_stale")
      return { label: "Task changed since", tone: "neutral" };
    if (row.status === "void_gone")
      return { label: "Task no longer exists", tone: "neutral" };
    return receiptSignal(row.status);
  }
  // An update row carries its digest in the second line; a bare count said
  // nothing about what changed.
  if (row.kind === "update") return null;
  if (row.kind === "inbox") {
    return (
      LOUD_SEVERITIES.get(String(row.severity ?? "").toLowerCase()) ?? null
    );
  }
  return null;
}

const SEVERITY_RANK = { critical: 3, high: 2, medium: 1, normal: 1, low: 0 };
const PRIORITY_RANK = { p0: 3, p1: 2, p2: 1, p3: 0 };

/**
 * A short, readable stand-in for an identifier that resolved to no name:
 * `agent_6400c2d2-…` reads as "agent 6400c2d2". The full id stays available
 * behind a copy affordance; it is never the label.
 */
export function shortIdLabel(value) {
  const raw = String(value ?? "")
    .trim()
    .replace(/^actor:/, "");
  if (!raw) return "";
  const match = raw.match(
    /^([a-z]+)[_-]?([0-9a-f]{8})(?:-?[0-9a-f]{4}){3}-?[0-9a-f]{12}$/i,
  );
  if (match) return `${match[1].toLowerCase()} ${match[2].toLowerCase()}`;
  const hex = raw.match(/^([0-9a-f]{8})(?:-?[0-9a-f]{4}){3}-?[0-9a-f]{12}$/i);
  if (hex) return `id ${hex[1].toLowerCase()}`;
  return raw.length > 28 ? `${raw.slice(0, 24)}…` : raw;
}

/**
 * How long something has waited on the reader, in the units an operator
 * reads at a glance: "41m", "3h 12m", "2d 4h".
 */
export function formatWait(ms) {
  const value = Number(ms);
  if (!Number.isFinite(value) || value < 0) return "";
  const minutes = Math.floor(value / 60_000);
  if (minutes < 1) return "<1m";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const rest = minutes % 60;
    return rest ? `${hours}h ${rest}m` : `${hours}h`;
  }
  const days = Math.floor(hours / 24);
  const restHours = hours % 24;
  return restHours ? `${days}d ${restHours}h` : `${days}d`;
}

/** Computed task age or elapsed ask wait, in milliseconds; NaN if unknown. */
export function rowWaitMs(row, now = Date.now()) {
  const age = row?.kind === "task" ? row.item?.work_summary?.age : undefined;
  if (Number.isFinite(age) && age >= 0) return age * 1000;
  const since = Date.parse(row?.waitingSince ?? "");
  return Number.isFinite(since) ? Math.max(0, now - since) : Number.NaN;
}

function humanizeSlug(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return "";
  const words = raw.replace(/[-_]+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * What an inbox item is about, in operator terms. A task wins: an ask filed
 * on a project that names one task is blocking that task, and that is what
 * the reader needs to see. Titles come from data already loaded; a slug is
 * humanized only as a last resort.
 */
export function inboxItemSubject(
  item,
  { titleFor = () => "", work = [] } = {},
) {
  const explicit = String(getInboxSubjectRef(item) ?? "").trim();
  const related = (Array.isArray(item?.related_refs) ? item.related_refs : [])
    .map((ref) => String(ref ?? "").trim())
    .filter(Boolean);
  const explicitPrefix = splitTypedRef(explicit).prefix;
  const ref =
    explicitPrefix === "card" || explicitPrefix === "document"
      ? explicit
      : related.find((candidate) => candidate.startsWith("card:")) ||
        explicit ||
        related.find((candidate) => candidate.startsWith("document:")) ||
        "";
  if (!ref) return null;
  const { prefix, id } = splitTypedRef(ref);
  const task =
    prefix === "card"
      ? work.find(
          (entry) =>
            workKey(entry) === ref || entry?.handle === id || entry?.id === id,
        ) || null
      : null;
  const title =
    String(task?.title ?? "").trim() ||
    titleFor(ref) ||
    (ref === explicit ? String(item?.subject_title ?? "").trim() : "") ||
    humanizeSlug(id);
  return {
    ref,
    kind: prefix,
    noun: inboxSubjectNoun(prefix),
    title,
    phase: task?.phase || "",
    phaseLabel: task?.phase ? phaseLabel(task.phase) : "",
    work: task,
  };
}

function severityRank(row) {
  const priority = String(
    row?.priority || row?.item?.priority || "",
  ).toLowerCase();
  return (
    PRIORITY_RANK[priority] ??
    SEVERITY_RANK[
      String(row?.severity ?? row?.item?.severity ?? "").toLowerCase()
    ] ??
    0
  );
}

function waitStartMinute(row) {
  const since = Date.parse(row?.waitingSince ?? "");
  return Number.isFinite(since) ? Math.floor(since / 60_000) : Number.NaN;
}

/** Explicit asks and decisions precede blocked tasks; priority precedes age. */
export function compareNeedsYou(a, b) {
  const task = Number(a.kind === "task") - Number(b.kind === "task");
  if (task) return task;
  const stale = Number(Boolean(a.stale)) - Number(Boolean(b.stale));
  if (stale) return stale;
  const priority = severityRank(b) - severityRank(a);
  if (priority) return priority;
  const startA = waitStartMinute(a);
  const startB = waitStartMinute(b);
  const hasA = Number.isFinite(startA);
  const hasB = Number.isFinite(startB);
  if (hasA !== hasB) return hasA ? -1 : 1;
  if (hasA && startA !== startB) return startA - startB;
  return String(a?.title ?? "").localeCompare(String(b?.title ?? ""));
}

/** No owner and no meaningful movement in over 30 days, using core's facts. */
export function isStaleBlockedTask(item, now = Date.now()) {
  const summary = item?.work_summary;
  if (!summary || !taskIsBlocked({ item })) return false;
  if (String(summary.owner || item.owner || "").trim()) return false;
  const moved = Date.parse(summary.last_movement_at || "");
  const inactive = Number.isFinite(moved) ? (now - moved) / 1000 : summary.age;
  return Number.isFinite(inactive) && inactive > 30 * 24 * 60 * 60;
}

/**
 * @param {object} input
 * @param {(id: string) => string} [input.agentName] host-derived agent
 *   name ("codex on workstation-a") for an actor id, or "" for anyone else. It
 *   outranks the requester label core stored with the ask.
 * @param {(id: string) => string} [input.actorName] display name for an
 *   actor id, or "" when the id resolves to no one.
 */
export function buildInboxRows({
  decisions = [],
  actions = [],
  receiptsUnavailable = false,
  work = [],
  inboxItems = [],
  updates = [],
  now = Date.now(),
  currentActorId = "",
  actorName = () => "",
  agentName = () => "",
  humanIds = new Set(),
} = {}) {
  const rows = [];
  const titles = new Map();
  const workByRef = new Map();
  for (const item of work) {
    if (!item || !workKey(item)) continue;
    workByRef.set(workKey(item), item);
    const title = String(item.title || "").trim();
    titles.set(workKey(item), title);
    // Inbox items may name a card by id rather than public ref.
    if (item.id) titles.set(`card:${item.id}`, title);
    if (item.handle) titles.set(`card:${item.handle}`, title);
  }
  for (const group of updates) {
    const ref = String(group?.group_ref ?? "").trim();
    const name = String(group?.display_name ?? "").trim();
    if (ref && name && !titles.has(ref)) titles.set(ref, name);
    for (const event of Array.isArray(group?.events) ? group.events : []) {
      const docRef = (Array.isArray(event?.refs) ? event.refs : []).find(
        (value) => String(value).startsWith("document:"),
      );
      const docTitle = String(event?.payload?.subject_title ?? "").trim();
      if (docRef && docTitle && !titles.has(docRef))
        titles.set(docRef, docTitle);
    }
  }
  const titleFor = (ref) => titles.get(String(ref ?? "").trim()) || "";
  // A blocked card with an explicit ask is already represented by that ask.
  // Keep the card out of Needs you after the ask is answered too: the answer
  // releases the human, while the agent still owns moving the card forward.
  const openAskedCardRefs = new Set();
  const answeredAskAtByCardRef = new Map();
  for (const raw of inboxItems) {
    const item = enrichInboxItem(raw);
    if (String(item?.kind ?? item?.category ?? "").trim() !== "ask") continue;
    const subject = inboxItemSubject(item, { titleFor, work });
    if (subject?.kind !== "card") continue;
    const task = subject.work;
    const refs = [
      subject.ref,
      task ? workKey(task) : "",
      task?.id ? `card:${task.id}` : "",
      task?.handle ? `card:${task.handle}` : "",
    ].filter(Boolean);
    if (inboxItemNeedsResponse(item)) {
      for (const ref of refs) openAskedCardRefs.add(ref);
      continue;
    }
    const answeredAt = Date.parse(item.responded_at || item.completed_at || "");
    if (!Number.isFinite(answeredAt)) continue;
    for (const ref of refs) {
      answeredAskAtByCardRef.set(
        ref,
        Math.max(answeredAskAtByCardRef.get(ref) || 0, answeredAt),
      );
    }
  }
  const nameFor = (id) => {
    const raw = String(id ?? "").trim();
    return raw ? String(actorName(raw) ?? "").trim() : "";
  };
  for (const item of decisions) {
    const summary = decisionSummary(item, titles.get(item.work_ref) || "");
    rows.push({
      id: `decision:${item.id}`,
      kind: "decision",
      // The task is what the row is about; the ask is the second line. The
      // raw work ref is pane-header material, never a list line.
      title: summary.title,
      // A trashed task has no title to lead with; the ask leads and the
      // second line says why the subject is gone.
      source:
        item.work_missing === true && !titles.get(item.work_ref)
          ? "Task no longer exists"
          : summary.ask || "Decision",
      ref: item.work_ref || "",
      time: item.updated_at || item.created_at,
      waitingSince: item.created_at || item.updated_at || "",
      status: decisionRowStatus(item, actions, {
        receiptsUnavailable,
        work: workByRef.get(item.work_ref) || null,
        currentActorId,
      }),
      phase: item.status,
      priority: item.priority || workByRef.get(item.work_ref)?.priority || "",
      item,
    });
  }
  for (const item of work) {
    const taskRefs = [
      workKey(item),
      item.ref,
      item.id ? `card:${item.id}` : "",
      item.handle ? `card:${item.handle}` : "",
    ];
    if (taskIsBlocked({ item })) {
      const hasOpenAsk = taskRefs.some(
        (ref) => ref && openAskedCardRefs.has(ref),
      );
      const answeredAt = Math.max(
        0,
        ...taskRefs.map((ref) => answeredAskAtByCardRef.get(ref) || 0),
      );
      const updatedAt = Date.parse(item.updated_at || "");
      const unchangedSinceAnswer =
        answeredAt > 0 &&
        (!Number.isFinite(updatedAt) || updatedAt <= answeredAt);
      if (hasOpenAsk || unchangedSinceAnswer) continue;
    }
    // A native task has no source worth naming; who owns it is the signal.
    const ownerId = String(
      item.work_summary?.owner || item.owner || "",
    ).replace(/^actor:/, "");
    const owner =
      ownerId && ownerId === currentActorId ? "you" : nameFor(ownerId);
    rows.push({
      id: `task:${workKey(item)}`,
      kind: "task",
      humanNext:
        !["done", "cancelled"].includes(
          item.work_summary?.status?.state || item.phase,
        ) && isHumanNextActor(item, humanIds),
      title: item.title || "Untitled task",
      source:
        String(item.source?.authority ?? "").toLowerCase() === "nexus"
          ? owner
            ? `Owned by ${owner}`
            : "Task"
          : sourceLabel(item.source),
      ref: item.ref || workKey(item),
      time: item.freshness?.last_observed_at || item.updated_at,
      waitingSince: item.work_summary?.created_at || item.updated_at || "",
      stale: isStaleBlockedTask(item, now),
      status: item.work_summary?.status?.state || item.phase,
      phase: item.work_summary?.status?.state || item.phase,
      item,
    });
  }
  for (const raw of inboxItems) {
    const item = enrichInboxItem(raw);
    const subject = inboxItemSubject(item, { titleFor, work });
    const requesterId =
      String(item.requester_actor_id ?? "").trim() ||
      String(item.requester_agent_id ?? "").trim();
    const requesterName =
      String(agentName(requesterId) ?? "").trim() ||
      String(item.requester_label ?? "").trim() ||
      nameFor(requesterId);
    const responderId = String(item.responding_actor_id ?? "").trim();
    rows.push({
      id: inboxItemMailboxId(item),
      kind: "inbox",
      // Core titles a completed row "Human response recorded: <ask>"; the
      // Handled mailbox already says it was answered.
      title:
        String(item.title || item.summary || "")
          .replace(/^Human response recorded:?\s*/i, "")
          .trim() || "Inbox item",
      // Name the subject when we know it; a raw ref is a last resort.
      source: subject ? `${subject.noun}: ${subject.title}` : "",
      subject,
      ref: item.subject_ref || "",
      time: item.source_event_time || item.created_at || item.responded_at,
      waitingSince:
        item.source_event_time || item.trigger_at || item.created_at || "",
      status: item.status || (item.responded_at ? "completed" : "open"),
      category: String(item.kind ?? item.category ?? "").trim(),
      severity: item.severity || "",
      requester: { name: requesterName, id: requesterId },
      requesterLabel: requesterName || shortIdLabel(requesterId),
      responder: responderId
        ? { name: nameFor(responderId), id: responderId }
        : null,
      body: item.body || "",
      responseProposals: Array.isArray(item.response_proposals)
        ? item.response_proposals
            .map((value) => String(value ?? "").trim())
            .filter(Boolean)
        : [],
      // Set only on an agent's request for a grant. The respond panel offers
      // the two decisions core accepts on those, and nothing else.
      access: accessRequestFromInboxItem(item),
      /*
       * Why the last send failed, written onto the item by the response queue.
       * Set means: nothing was recorded, and the reader has to decide again.
       */
      responseError: String(item.response_error ?? "").trim(),
      item,
    });
  }
  for (const group of updates) {
    const digest = updateDigest(group.events, {
      actorName: (id) => nameFor(id) || shortIdLabel(id),
      titleFor,
      unreadCount: Number(group.unread_count) || 0,
      groupRef: group.group_ref,
      isAgent: (id) => Boolean(agentName(id)),
      selfId: currentActorId,
    });
    rows.push({
      id: `update:${group.group_ref || group.display_name}`,
      kind: "update",
      title: group.display_name || group.group_ref || "Update",
      source: digest || "New activity",
      ref: group.group_ref || "",
      time: group.newest_event?.ts,
      status: "update",
      count: group.unread_count,
      grouped: true,
      item: group,
    });
  }
  const classified = rows
    .map((row) => ({ ...row, mailbox: classifyInboxRow(row, now) }))
    .filter((row) => row.mailbox !== null)
    .sort((a, b) => {
      if (a.mailbox === "watching" && b.mailbox === "watching") {
        const importance = (row) =>
          row.kind === "decision"
            ? 3
            : row.kind === "task"
              ? 2
              : Math.max(
                  0,
                  ...(row.item?.events || []).map((event) => {
                    if (
                      [
                        "human_attention_requested",
                        "human_attention_responded",
                      ].includes(event.type)
                    )
                      return 3;
                    if (
                      event.type === "card_resolved" ||
                      (event.type === "card_moved" &&
                        ["done", "blocked"].includes(event.payload?.column_key))
                    )
                      return 2;
                    return 0;
                  }),
                );
        const delta = importance(b) - importance(a);
        if (delta) return delta;
      }
      return rowTime(b) - rowTime(a);
    });
  const needsYou = classified
    .filter((row) => row.mailbox === "needs-you")
    .sort(compareNeedsYou);
  return [
    ...needsYou,
    ...classified.filter((row) => row.mailbox !== "needs-you"),
  ];
}

// Newest first across kinds; a row without a time sorts last, in input order.
function rowTime(row) {
  const t = Date.parse(row?.time ?? "");
  return Number.isFinite(t) ? t : Number.NEGATIVE_INFINITY;
}

export function filterMailbox(rows, mailbox) {
  return rows.filter((row) => row.mailbox === mailbox);
}

/**
 * Rows about one task, for `?work_ref=` links ("Inbox for this task"). An
 * inbox item matches when the task is its subject or one of its refs.
 */
export function rowMatchesWorkRef(row, workRef) {
  const ref = String(workRef ?? "").trim();
  if (!ref) return true;
  if (row?.ref === ref || row?.subject?.ref === ref) return true;
  if (row?.kind === "decision") return row.item?.work_ref === ref;
  if (row?.kind === "inbox") {
    const related = Array.isArray(row.item?.related_refs)
      ? row.item.related_refs
      : [];
    return related.some((value) => String(value).trim() === ref);
  }
  return false;
}
