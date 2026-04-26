import { writable } from "svelte/store";

/**
 * Synchronises hover state between a document-body `<mark data-event-id="…">`
 * and the matching timeline `MessageItem` (`#message-…`) for anchored comments.
 */
export const docCommentBodyHover = writable(
  /** @type {string | null} */ (null),
);
