/**
 * Canonical POST /topics bodies for UI callers. Core create requires
 * type, title, summary, ref arrays, and provenance. Topic `state` is derived server-side.
 */

/**
 * Topic create payload from the Topics list page draft.
 *
 * @param {{ title: string, summary: string, type?: string }} draft
 * @returns {{ topic: Record<string, unknown> }}
 */
export function buildTopicCreatePayloadFromDraft(draft) {
  const summary = String(draft.summary ?? "").trim() || "No summary provided.";
  const type = String(draft.type ?? "other").trim() || "other";
  return {
    topic: {
      type,
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
