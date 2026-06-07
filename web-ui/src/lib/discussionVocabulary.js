/**
 * Canonical, cross-surface vocabulary for the unified Discussion model.
 *
 * Mental model (shared by operators and agents):
 *   Boards, Cards, Topics, and Docs each have exactly one Discussion.
 *   A Discussion is a thread of Messages.
 *   A Comment is a Message anchored to a text selection in a Doc.
 *
 * Every messaging surface pulls its user-facing copy from here so the nouns
 * cannot drift again (board "announcements" vs topic "messages" vs doc
 * "comments" vs the "Post message" / "Comment" button split). Surface-specific
 * builders live in `discussionSurface.js`; this module owns the words.
 */

/** Section / panel title used everywhere a Discussion is shown. */
export const DISCUSSION_TITLE = "Discussion";

/** Secondary panel title for document revision history. */
export const REVISIONS_TITLE = "Revisions";

/** Composer placeholder for an ordinary message. */
export const COMPOSER_PLACEHOLDER = "Write a message…";

/** Composer placeholder while drafting an anchored document comment. */
export const COMPOSER_COMMENT_PLACEHOLDER =
  "Add a comment, or @mention an agent…";

/** Primary send action label, used on every surface. */
export const SEND_LABEL = "Send";

/** In-flight send label (single ellipsis style per web-ui copy guide). */
export const SENDING_LABEL = "Sending…";

/**
 * Send label for an anchored document comment. A comment is still a message;
 * the distinct verb only reflects that it is pinned to a text selection.
 */
export const COMMENT_SEND_LABEL = "Comment";

/** Loading state for the message list. */
export const LOADING_MESSAGES = "Loading messages…";

/** Default zero-state when a Discussion has no messages yet. */
export const EMPTY_DEFAULT =
  "No messages yet. Post the first message to start the conversation.";

/** Zero-state shown when archived messages are hidden and none are visible. */
export const EMPTY_ALL_ARCHIVED =
  "No messages in view. Turn on Show archived to see archived messages.";

/**
 * Count noun helper. Returns e.g. "1 message" / "3 messages". Used for badges,
 * aria labels, and any "N messages" affordance so the unit is always "message".
 */
export function messageCountLabel(count) {
  const n = Number.isFinite(count) ? Math.max(0, Math.floor(count)) : 0;
  return `${n} ${n === 1 ? "message" : "messages"}`;
}

/** Board Discussion empty-state copy. */
export const BOARD_EMPTY =
  "Board-wide updates and discussion live here, separate from individual card threads. Post a short note the whole board should see — triage callouts, column policy, or sprint boundaries.";

/**
 * Document Discussion empty-state copy. Doubles as a discoverability hint for
 * the text-anchored comment gesture (Mod+Opt+M matches Google Docs).
 */
export const DOC_EMPTY =
  "No comments yet. Select text in the doc and press ⌘⌥M (Ctrl+Alt+M) to comment, or write a freeform note below.";

/** Topic Discussion empty-state copy. */
export function topicEmpty(topicTitle) {
  const title = String(topicTitle ?? "").trim() || "this topic";
  return `Everything about ${title} lives here. Post a message to start the conversation. Docs and Boards you link to this topic appear in their tabs.`;
}
