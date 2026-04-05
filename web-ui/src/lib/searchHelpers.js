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

export async function searchBoards(query, limit = 20) {
  const response = await coreClient.listBoards({
    q: query,
    limit,
  });
  return response.boards || [];
}

export async function searchArtifacts(query, limit = 20) {
  const response = await coreClient.listArtifacts({
    q: query,
    limit,
  });
  return response.artifacts || [];
}
