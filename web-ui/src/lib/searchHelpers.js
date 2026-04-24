import { coreClient } from "./coreClient.js";
import { filterTopLevelDocuments } from "./documentVisibility.js";

/**
 * Resolves the backing collaboration thread id for topic search results.
 * Topic primary key (`id`) may differ from `thread_id`; board and document
 * flows that attach to a backing thread must prefer `thread_id` when set.
 */
export function backingThreadIdFromTopicRecord(topic) {
  if (!topic || typeof topic !== "object") {
    return "";
  }
  const fromThread = String(topic.thread_id ?? "").trim();
  if (fromThread) {
    return fromThread;
  }
  return String(topic.id ?? "").trim();
}

/** Option shape for SearchableEntityPicker when listing topics as thread anchors. */
export function topicSearchResultToPickerOption(topic) {
  const id = backingThreadIdFromTopicRecord(topic);
  return {
    id,
    title: topic.title || id,
    subtitle: [topic.status, topic.priority].filter(Boolean).join(" · "),
    keywords: [topic.type, ...(topic.tags ?? [])],
  };
}

/**
 * Option shape for linking a board to a topic via typed ref (`topic:<id>`).
 * Prefer this for board create; do not use the topic's thread_id as board.thread_id.
 */
export function topicSearchResultToBoardRefOption(topic) {
  if (!topic || typeof topic !== "object") {
    return { id: "", title: "", subtitle: "", keywords: [] };
  }
  const rawId = String(topic.id ?? "").trim();
  if (!rawId) {
    return {
      id: "",
      title: String(topic.title ?? "").trim() || "",
      subtitle: "",
      keywords: [topic.type, ...(topic.tags ?? [])],
    };
  }
  const typedRef = rawId.includes(":") ? rawId : `topic:${rawId}`;
  const timeline = backingThreadIdFromTopicRecord(topic);
  const subtitleParts = [topic.status, topic.priority];
  if (timeline) {
    subtitleParts.push(`Timeline ${timeline}`);
  }
  return {
    id: typedRef,
    title: topic.title || typedRef,
    subtitle: subtitleParts.filter(Boolean).join(" · "),
    keywords: [topic.type, ...(topic.tags ?? [])],
  };
}

export async function searchTopics(query, limit = 20) {
  const response = await coreClient.listTopics({
    q: query,
    limit,
  });
  return response.topics || [];
}

export async function searchDocuments(query, limit = 20) {
  const response = await coreClient.listDocuments({
    q: query,
    limit,
  });
  return filterTopLevelDocuments(response.documents);
}

export async function searchActors(query, limit = 20) {
  const response = await coreClient.listActors({
    q: query,
    limit,
  });
  return response.actors || [];
}

/**
 * boards.list returns rows shaped as `{ board, summary }`. Callers that need a
 * flat board record (search palette, pickers) must read the inner `board` map.
 */
export function boardRecordFromBoardsListRow(row) {
  if (!row || typeof row !== "object") {
    return row;
  }
  if (row.board && typeof row.board === "object") {
    return row.board;
  }
  return row;
}

export async function searchBoards(query, limit = 20) {
  const response = await coreClient.listBoards({
    q: query,
    limit,
  });
  const rows = response.boards || [];
  return rows.map(boardRecordFromBoardsListRow);
}

export async function searchArtifacts(query, limit = 20) {
  const response = await coreClient.listArtifacts({
    q: query,
    limit,
  });
  return response.artifacts || [];
}
