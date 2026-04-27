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

/**
 * Persistent body/rail selection set by clicking a document highlight.
 * Hover remains transient; focus stays long enough for the operator to see
 * which rail card the body text opened.
 */
export const docCommentBodyFocus = writable(
  /** @type {string[] | null} */ (null),
);
