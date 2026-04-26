import { writable } from "svelte/store";

/**
 * Synchronises hover state between document-body `mark.js-doc-comment-mark`
 * (with `data-event-id` and optional `data-event-ids` for stacked comments)
 * and matching timeline `MessageItem` (`#message-…`) for anchored comments.
 * Multiple ids are used when the body mark anchors several comments on the
 * same quoted range.
 */
export const docCommentBodyHover = writable(
  /** @type {string[] | null} */ (null),
);
