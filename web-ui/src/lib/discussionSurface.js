/**
 * Declarative descriptors for the four Discussion surfaces (board, card,
 * topic, document).
 *
 * Each builder returns a plain object whose keys mirror DiscussionDrawer props,
 * so a wrapper can spread it directly:
 *
 *   <DiscussionDrawer {...boardDiscussionSurface(board)} {workspaceId} … />
 *
 * This is the single seam where surfaces are allowed to differ. Anything that
 * is not captured here (label, layout, timeline source, ref filters, lifecycle
 * wording, live updates) should be added to the descriptor rather than
 * hardcoded in a per-surface wrapper, so the surfaces stay consistent.
 */

import { boardBackingThreadId } from "$lib/boardUtils";
import {
  BOARD_EMPTY,
  DISCUSSION_TITLE,
  DOC_EMPTY,
  topicEmpty,
} from "$lib/discussionVocabulary";

/**
 * @typedef {"board" | "card" | "topic" | "document"} DiscussionSurfaceKind
 *
 * @typedef {Object} DiscussionSurface
 * @property {DiscussionSurfaceKind} kind        Which primitive this Discussion belongs to.
 * @property {string} threadId                   Backing thread id for the Discussion.
 * @property {string} label                      Panel/section title (always "Discussion").
 * @property {"dock"|"rail"|"primary"} layout    Formal layout mode (see DiscussionDrawer).
 * @property {string} storageKey                 localStorage namespace for open/size prefs.
 * @property {string} emptyMessage               Zero-state copy.
 * @property {"thread"|"topic"} timelineSource    Where the timeline comes from.
 * @property {boolean} liveUpdates               Subscribe to SSE for live updates (isolated context only).
 * @property {string} [subjectRefFilter]         Only show events whose refs include this.
 * @property {string[]} [extraPostRefs]          Extra refs appended to posted messages.
 * @property {"archive"|"resolve"} [archiveLabelKind] Lifecycle wording per message.
 * @property {boolean} [expandFillsParent]
 * @property {boolean} [narrowEdgeToEdge]
 */

/**
 * Board-wide Discussion (dock under the board viewport).
 * @returns {DiscussionSurface}
 */
export function boardDiscussionSurface(board) {
  const threadId = boardBackingThreadId(board);
  return {
    kind: "board",
    threadId,
    label: DISCUSSION_TITLE,
    layout: "dock",
    storageKey: `board-feed:${threadId}`,
    emptyMessage: BOARD_EMPTY,
    timelineSource: "thread",
    liveUpdates: true,
    expandFillsParent: true,
    narrowEdgeToEdge: true,
  };
}

/**
 * Per-card Discussion (dock inside the card detail view/modal).
 * @param {{ threadId: string, cardKey: string }} args
 * @returns {DiscussionSurface}
 */
export function cardDiscussionSurface({ threadId, cardKey }) {
  return {
    kind: "card",
    threadId: String(threadId ?? "").trim(),
    label: DISCUSSION_TITLE,
    layout: "dock",
    storageKey: `card-discussion:${cardKey}`,
    emptyMessage: "",
    timelineSource: "thread",
    liveUpdates: true,
    expandFillsParent: true,
    narrowEdgeToEdge: true,
  };
}

/**
 * Document Discussion (right rail on desktop, dock on mobile). Doc messages can
 * be anchored to a text selection, so the lifecycle wording is Resolve/Reopen.
 * @returns {DiscussionSurface}
 */
export function documentDiscussionSurface(doc) {
  const docId = String(doc?.id ?? "").trim();
  const threadId = String(doc?.thread_id ?? "").trim();
  const documentRef = docId ? `document:${docId}` : "";
  return {
    kind: "document",
    threadId,
    label: DISCUSSION_TITLE,
    layout: "rail",
    storageKey: `doc-discussion:${docId}`,
    emptyMessage: DOC_EMPTY,
    timelineSource: "thread",
    liveUpdates: true,
    subjectRefFilter: documentRef,
    extraPostRefs: documentRef ? [documentRef] : [],
    archiveLabelKind: "resolve",
    expandFillsParent: true,
    narrowEdgeToEdge: true,
  };
}

/**
 * Topic Discussion (primary pane; the conversation is the artifact). The topic
 * page owns its own timeline store + SSE, so `liveUpdates` here is descriptive;
 * the drawer's isolated SSE path is skipped under `useParentTimelineContext`.
 * @returns {DiscussionSurface}
 */
export function topicDiscussionSurface(topic) {
  return {
    kind: "topic",
    threadId: String(topic?.id ?? "").trim(),
    label: DISCUSSION_TITLE,
    layout: "primary",
    storageKey: "",
    emptyMessage: topicEmpty(topic?.title),
    timelineSource: "topic",
    liveUpdates: true,
    narrowEdgeToEdge: true,
  };
}
