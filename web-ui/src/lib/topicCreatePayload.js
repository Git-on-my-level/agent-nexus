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
 * Minimal valid topic create payload (e.g. inbox tour, quick-create).
 *
 * @param {object} opts
 * @param {string} opts.title
 * @param {string} [opts.summary] — when empty, defaults to "No summary provided."
 * @param {string} [opts.type] — default "other"
 * @param {string} [opts.status] — default "active"
 * @param {string[]} [opts.provenanceSources] — default ["actor_statement:ui"]
 * @returns {{ topic: Record<string, unknown> }}
 */
export function buildTopicCreatePayloadForUi(opts) {
  const title = String(opts.title ?? "").trim();
  const summaryRaw = String(opts.summary ?? "").trim();
  const summary = summaryRaw || "No summary provided.";
  return {
    topic: {
      type: opts.type ?? "other",
      status: opts.status ?? "active",
      title,
      summary,
      owner_refs: [],
      document_refs: [],
      board_refs: [],
      related_refs: [],
      provenance: {
        sources: opts.provenanceSources ?? ["actor_statement:ui"],
      },
    },
  };
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
