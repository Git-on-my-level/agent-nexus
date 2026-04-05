import { splitTypedRef } from "$lib/inboxUtils";

/**
 * Canonical backing-thread id for board card rows / create payloads (`thread_id` only).
 */
export function resolveBoardCardThreadIdField(row) {
  const r = row && typeof row === "object" ? row : {};
  return String(r.thread_id ?? "").trim();
}

function encodeRouteSegment(value) {
  return encodeURIComponent(String(value ?? "").trim());
}

export function topicDetailPathFromRef(refValue) {
  const { prefix, id } = splitTypedRef(String(refValue ?? "").trim());
  if (prefix === "topic" && id) {
    return `/topics/${encodeRouteSegment(id)}`;
  }
  if (prefix === "thread" && id) {
    return `/threads/${encodeRouteSegment(id)}`;
  }
  return "";
}

export function topicDetailPathFromSubject({
  topicId,
  topicRef,
  subjectRef,
  relatedRefs,
  threadId,
} = {}) {
  const explicitTopicId = String(topicId ?? "").trim();
  if (explicitTopicId) {
    return `/topics/${encodeRouteSegment(explicitTopicId)}`;
  }

  const candidates = [
    topicRef,
    subjectRef,
    ...(Array.isArray(relatedRefs) ? relatedRefs : []),
  ];
  for (const candidate of candidates) {
    const path = topicDetailPathFromRef(candidate);
    if (path) {
      return path;
    }
  }

  const explicitThreadId = String(threadId ?? "").trim();
  if (explicitThreadId) {
    return `/threads/${encodeRouteSegment(explicitThreadId)}`;
  }

  return "";
}

/**
 * Path segment for `/topics/:segment` from a backing-thread inspect payload.
 * Prefers `thread.topic_ref` when it is a `topic:` ref; otherwise uses `thread.id`.
 */
export function topicRouteSegmentFromBackingThread(thread) {
  if (!thread || typeof thread !== "object") return "";
  const { prefix, id } = splitTypedRef(String(thread.topic_ref ?? "").trim());
  if (prefix === "topic" && id) return id;
  return String(thread.id ?? "").trim();
}

/**
 * Canonical `topic:<id>` ref for a board: prefer ordered `board.refs`, fall back to `primary_topic_ref`.
 */
export function boardPrimaryTopicRef(board) {
  const b = board && typeof board === "object" ? board : {};
  const refs = Array.isArray(b.refs) ? b.refs : [];
  for (const raw of refs) {
    const p = splitTypedRef(String(raw ?? "").trim());
    if (p.prefix === "topic" && p.id) {
      return `topic:${p.id}`;
    }
  }
  return String(b.primary_topic_ref ?? "").trim();
}

/** Whether the board is associated with the given topic id (refs scan, then legacy primary ref). */
export function boardOwnsTopicId(board, topicId) {
  const tid = String(topicId ?? "").trim();
  if (!tid) return false;
  const refs = Array.isArray(board?.refs) ? board.refs : [];
  for (const raw of refs) {
    const p = splitTypedRef(String(raw ?? "").trim());
    if (p.prefix === "topic" && p.id === tid) return true;
  }
  const legacy = splitTypedRef(String(board?.primary_topic_ref ?? "").trim());
  return legacy.prefix === "topic" && legacy.id === tid;
}

export function topicRouteSegmentFromBoardCardRow(membership, backingThread) {
  const nav = boardCardInspectNav(membership, backingThread);
  return nav ? nav.segment : "";
}

/**
 * Navigation target for a board card title link: topic detail vs backing-thread detail.
 * @returns {{ kind: 'topic' | 'thread', segment: string } | null}
 */
export function boardCardInspectNav(membership, backingThread) {
  const m = membership && typeof membership === "object" ? membership : {};
  const fromMembership = splitTypedRef(String(m.topic_ref ?? "").trim());
  if (fromMembership.prefix === "topic" && fromMembership.id) {
    return { kind: "topic", segment: fromMembership.id };
  }

  const refs = Array.isArray(m.related_refs) ? m.related_refs : [];
  for (const raw of refs) {
    const p = splitTypedRef(String(raw ?? "").trim());
    if (p.prefix === "topic" && p.id) return { kind: "topic", segment: p.id };
  }

  const bt =
    backingThread && typeof backingThread === "object" ? backingThread : null;
  const topicRefOnThread = splitTypedRef(String(bt?.topic_ref ?? "").trim());
  if (topicRefOnThread.prefix === "topic" && topicRefOnThread.id) {
    return { kind: "topic", segment: topicRefOnThread.id };
  }

  const threadIdFromBacking = String(bt?.id ?? "").trim();
  if (threadIdFromBacking)
    return { kind: "thread", segment: threadIdFromBacking };

  const threadIdFromRow = resolveBoardCardThreadIdField(m);
  if (threadIdFromRow) return { kind: "thread", segment: threadIdFromRow };

  return null;
}

/**
 * Board header / context line: canonical topic id for linking to `/topics/...`.
 */
export function topicRouteSegmentFromBoardWorkspace(workspace) {
  const nav = boardWorkspaceInspectNav(workspace);
  return nav ? nav.segment : "";
}

/**
 * Board workspace header: topic id vs backing thread id for correct /topics vs /threads links.
 * @returns {{ kind: 'topic' | 'thread', segment: string } | null}
 */
export function boardWorkspaceInspectNav(workspace) {
  const ws = workspace && typeof workspace === "object" ? workspace : {};
  const primary = String(ws.primary_topic?.id ?? "").trim();
  if (primary) return { kind: "topic", segment: primary };

  const board = ws.board && typeof ws.board === "object" ? ws.board : {};
  const fromBoardRef = splitTypedRef(boardPrimaryTopicRef(board));
  if (fromBoardRef.prefix === "topic" && fromBoardRef.id) {
    return { kind: "topic", segment: fromBoardRef.id };
  }

  const bt = ws.backing_thread;
  const topicRefOnThread = splitTypedRef(String(bt?.topic_ref ?? "").trim());
  if (topicRefOnThread.prefix === "topic" && topicRefOnThread.id) {
    return { kind: "topic", segment: topicRefOnThread.id };
  }

  const threadId = String(bt?.id ?? board.thread_id ?? "").trim();
  if (threadId) return { kind: "thread", segment: threadId };

  return null;
}

/**
 * Inbox/workspace warning row: prefer explicit topic_id for /topics; else thread_id → /threads.
 * @returns {{ kind: 'topic' | 'thread', segment: string } | null}
 */
export function warningInspectNav(warning) {
  const w = warning && typeof warning === "object" ? warning : {};
  const topicId = String(w.topic_id ?? "").trim();
  if (topicId) return { kind: "topic", segment: topicId };
  const threadId = String(w.thread_id ?? "").trim();
  if (threadId) return { kind: "thread", segment: threadId };
  return null;
}

/**
 * Board list row: topic ref vs backing thread for labels and /topics vs /threads links.
 * @returns {{ kind: 'topic' | 'thread', segment: string, display: string } | null}
 */
export function boardRowInspectNav(board) {
  const b = board && typeof board === "object" ? board : {};
  const refRaw = boardPrimaryTopicRef(b);
  const p = splitTypedRef(String(refRaw ?? "").trim());
  if (p.prefix === "topic" && p.id) {
    const display = refRaw || `topic:${p.id}`;
    return { kind: "topic", segment: p.id, display };
  }
  const threadId = String(b.thread_id ?? "").trim();
  if (threadId) return { kind: "thread", segment: threadId, display: threadId };
  return null;
}

/**
 * Inbox: prefer explicit `topic_id`, then `topic:` subject resolution.
 */
export function inboxTopicRouteSegment(item) {
  const row = item && typeof item === "object" ? item : {};
  const explicit = String(row.topic_id ?? "").trim();
  if (explicit) return explicit;

  const subject = String(row.subject_ref ?? "").trim();
  if (subject) {
    const p = splitTypedRef(subject);
    if (p.prefix === "topic" && p.id) return p.id;
  }
  return "";
}
