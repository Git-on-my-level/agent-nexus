/**
 * Canonical POST /topics bodies for UI callers. Core create requires
 * type, status, title, summary, ref arrays, and provenance.
 */

/**
 * Map thread-list UI status to canonical topic.status for POST /topics.
 * @param {string} [status]
 * @returns {string}
 */
export function mapThreadStatusToTopicStatus(status) {
  switch (String(status ?? "").trim()) {
    case "paused":
      return "blocked";
    case "closed":
      return "resolved";
    default:
      return "active";
  }
}

/**
 * Topic create payload from the Topics list page draft (includes thread-status mapping).
 *
 * @param {{ title: string, summary: string, status: string }} draft
 * @returns {{ topic: Record<string, unknown> }}
 */
export function buildTopicCreatePayloadFromDraft(draft) {
  const summary = String(draft.summary ?? "").trim() || "No summary provided.";
  return {
    topic: {
      type: "other",
      status: mapThreadStatusToTopicStatus(draft.status),
      title: String(draft.title ?? "").trim(),
      summary,
      owner_refs: [],
      document_refs: [],
      board_refs: [],
      related_refs: [],
      provenance: { sources: ["actor_statement:ui"] },
    },
  };
}
