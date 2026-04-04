/**
 * Maps a threads.workspace-shaped payload to TopicWorkspaceResponse fields
 * for mock servers and tests when the UI loads topic detail via topics.workspace.
 */

const TOPIC_TYPES = new Set([
  "initiative",
  "objective",
  "decision",
  "incident",
  "risk",
  "request",
  "note",
  "other",
]);

function threadTypeToTopicType(type) {
  const t = String(type ?? "").trim();
  if (TOPIC_TYPES.has(t)) return t;
  return "other";
}

function threadStatusToTopicStatus(status) {
  switch (String(status ?? "").trim()) {
    case "paused":
      return "blocked";
    case "closed":
      return "resolved";
    default:
      return "active";
  }
}

export function threadWorkspaceToTopicWorkspace(ws, topicIdOverride) {
  if (!ws || typeof ws !== "object") {
    return {
      topic: {},
      cards: [],
      boards: [],
      documents: [],
      threads: [],
      inbox: [],
      projection_freshness: {},
      generated_at: new Date().toISOString(),
    };
  }

  const thread = ws.thread && typeof ws.thread === "object" ? ws.thread : null;
  const context =
    ws.context && typeof ws.context === "object" ? ws.context : {};
  const documents = Array.isArray(context.documents) ? context.documents : [];
  const boardMemberships = Array.isArray(ws.board_memberships?.items)
    ? ws.board_memberships.items
    : [];
  const ownedItems = Array.isArray(ws.owned_boards?.items)
    ? ws.owned_boards.items
    : [];

  const boards = [];
  const boardIds = new Set();

  const threadId = thread ? String(thread.id ?? "").trim() : "";
  const topicId = String(topicIdOverride ?? "").trim() || threadId;

  for (const ob of ownedItems) {
    const bid = String(ob?.id ?? "").trim();
    if (!bid || boardIds.has(bid)) continue;
    boardIds.add(bid);
    boards.push({
      id: bid,
      title: ob.title,
      status: ob.status,
      primary_topic_ref: topicId ? `topic:${topicId}` : "",
      updated_at: ob.updated_at,
    });
  }

  for (const m of boardMemberships) {
    const b = m?.board;
    const bid = String(b?.id ?? m?.board_id ?? "").trim();
    if (bid && !boardIds.has(bid)) {
      boardIds.add(bid);
      boards.push({
        id: bid,
        title: b?.title,
        status: b?.status,
      });
    }
  }

  const cards = [];
  for (const m of boardMemberships) {
    const c = m?.card;
    if (!c || typeof c !== "object") continue;
    const bid = String(c.board_id ?? m?.board?.id ?? "").trim();
    if (!bid) continue;
    cards.push({
      ...c,
      board_id: c.board_id || bid,
      thread_id: c.thread_id || thread?.id,
    });
  }

  const topic = thread
    ? {
        id: topicId,
        type: threadTypeToTopicType(thread.type),
        status: threadStatusToTopicStatus(thread.status),
        title: thread.title,
        summary: String(thread.current_summary ?? ""),
        owner_refs: Array.isArray(thread.owner_refs) ? thread.owner_refs : [],
        thread_id: threadId || null,
        document_refs: Array.isArray(thread.document_refs)
          ? thread.document_refs
          : [],
        board_refs: Array.isArray(thread.board_refs) ? thread.board_refs : [],
        related_refs: Array.isArray(thread.related_refs)
          ? thread.related_refs
          : [],
        created_at: thread.created_at ?? thread.updated_at,
        created_by: thread.created_by ?? thread.updated_by,
        updated_at: thread.updated_at,
        updated_by: thread.updated_by,
        provenance:
          thread.provenance && typeof thread.provenance === "object"
            ? thread.provenance
            : { sources: [] },
      }
    : {};

  const threadWithTopicRef = thread
    ? {
        ...thread,
        topic_ref: topicId ? `topic:${topicId}` : thread.topic_ref,
      }
    : null;

  return {
    topic,
    cards,
    boards,
    documents,
    threads: threadWithTopicRef ? [threadWithTopicRef] : [],
    inbox: Array.isArray(ws.inbox?.items) ? ws.inbox.items : [],
    projection_freshness:
      ws.projection_freshness && typeof ws.projection_freshness === "object"
        ? ws.projection_freshness
        : { aggregate: "unknown" },
    generated_at:
      typeof ws.generated_at === "string"
        ? ws.generated_at
        : new Date().toISOString(),
  };
}
