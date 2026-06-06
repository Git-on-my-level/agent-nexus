import {
  boardCardPlacementAnchorId,
  boardCardStableId,
  sortedColumnPeersStableIds,
} from "./boardUtils.js";
import { resourceRouteSegment } from "./resourceIdentity.js";
import { validateEventRefRule } from "./eventRefRules.js";

/** @param {string} ref */
export function refIsTerminalEvidence(ref) {
  const s = String(ref ?? "")
    .trim()
    .toLowerCase();
  return s.startsWith("artifact:") || s.startsWith("event:");
}

/** @param {object | null | undefined} membership @param {string[]} [extraRefs] */
export function cardHasTerminalEvidence(membership, extraRefs = []) {
  const membRefs = [...(membership?.resolution_refs ?? [])]
    .map((r) => String(r ?? "").trim())
    .filter(Boolean);
  const merged = [...membRefs, ...extraRefs].filter(Boolean);
  return merged.some(refIsTerminalEvidence);
}

/**
 * @param {object} opts
 * @param {object} opts.cardItem
 * @param {object | null | undefined} opts.board
 * @param {string} opts.boardId
 * @param {string} [opts.note]
 */
export function buildCardResolvedAttestationEvent({
  cardItem,
  board,
  boardId,
  note = "",
}) {
  const membership = cardItem?.membership ?? {};
  const cardSegment =
    resourceRouteSegment(membership, "card") || boardCardStableId(membership);
  const boardSegment =
    resourceRouteSegment(board, "board") || String(boardId ?? "").trim();
  const threadId = String(membership?.thread_id ?? "").trim();
  const title = String(membership?.title ?? "").trim() || cardSegment;
  const trimmedNote = String(note ?? "").trim();

  const refs = [
    cardSegment ? `card:${cardSegment}` : "",
    boardSegment ? `board:${boardSegment}` : "",
    threadId ? `thread:${threadId}` : "",
  ].filter(Boolean);

  const payload = {
    card_id: cardSegment,
    resolution: "done",
  };
  if (trimmedNote) {
    payload.note = trimmedNote;
  }

  const event = {
    type: "card_resolved",
    thread_id: threadId,
    refs,
    summary: trimmedNote
      ? `Marked done: ${trimmedNote.slice(0, 120)}`
      : `Marked done: ${title}`,
    payload,
    provenance: { sources: ["event:ui-attestation"] },
  };

  const validation = validateEventRefRule(event.type, refs, payload);
  if (!validation.valid) {
    throw new Error(validation.error || "Invalid card_resolved event");
  }

  return event;
}

/**
 * Compute `before_card_id` for cards.move from a drop index in the target column.
 * @param {string[]} peerStableIds ordered peers in target column (excluding dragged card)
 * @param {number} insertIndex 0-based index where card should land
 */
export function beforeCardIdForInsert(peerStableIds, insertIndex) {
  const peers = peerStableIds ?? [];
  if (insertIndex <= 0) {
    return peers[0] ? String(peers[0]) : "";
  }
  if (insertIndex >= peers.length) {
    return "";
  }
  return String(peers[insertIndex] ?? "");
}

/**
 * Resolve a UI card key (handle/public segment) to a cards.move placement anchor.
 * @param {{ items?: unknown[] } | null | undefined} cardsSection
 * @param {string} token
 */
export function resolveBoardCardPlacementAnchor(cardsSection, token) {
  const needle = String(token ?? "").trim();
  if (!needle) return "";
  for (const item of cardsSection?.items ?? []) {
    const membership = item?.membership;
    if (!membership) continue;
    const anchor = boardCardPlacementAnchorId(membership);
    if (needle === anchor || boardCardStableId(membership) === needle) {
      return anchor;
    }
  }
  return needle;
}

/**
 * Normalize cards.move placement anchors to canonical card row ids.
 * @param {{ items?: unknown[] } | null | undefined} cardsSection
 * @param {Record<string, unknown>} payload
 */
export function normalizeBoardMovePlacementAnchors(cardsSection, payload) {
  const next = { ...payload };
  if ("before_card_id" in next) {
    next.before_card_id = resolveBoardCardPlacementAnchor(
      cardsSection,
      next.before_card_id,
    );
  }
  if ("after_card_id" in next) {
    next.after_card_id = resolveBoardCardPlacementAnchor(
      cardsSection,
      next.after_card_id,
    );
  }
  return next;
}

/** @param {object | null | undefined} item @param {string} token */
function cardMatchesMoveToken(item, token) {
  const membership = item?.membership;
  if (!membership) return false;
  return (
    boardCardStableId(membership) === token ||
    boardCardPlacementAnchorId(membership) === token
  );
}

/**
 * @param {object | null | undefined} workspace
 * @param {string} cardStableId
 * @param {{ column_key: string, before_card_id?: string }} move
 */
export function applyOptimisticCardMove(workspace, cardStableId, move) {
  if (!workspace?.cards?.items) return workspace;

  const columnKey = String(move.column_key ?? "").trim();
  if (!columnKey) return workspace;

  const items = [...workspace.cards.items];
  const fromIdx = items.findIndex((c) => cardMatchesMoveToken(c, cardStableId));
  if (fromIdx < 0) return workspace;

  const [card] = items.splice(fromIdx, 1);
  const updatedMembership = {
    ...card.membership,
    column_key: columnKey,
    updated_at: new Date().toISOString(),
  };
  const updatedCard = { ...card, membership: updatedMembership };

  const columnPeers = items
    .filter((c) => String(c?.membership?.column_key ?? "") === columnKey)
    .sort((a, b) => {
      const ra = Number.parseInt(String(a?.membership?.rank ?? "0"), 10);
      const rb = Number.parseInt(String(b?.membership?.rank ?? "0"), 10);
      if (ra !== rb) return ra - rb;
      return String(a?.membership?.created_at ?? "").localeCompare(
        String(b?.membership?.created_at ?? ""),
      );
    });

  const beforeId = String(move.before_card_id ?? "").trim();
  let insertIdx = items.length;
  if (beforeId) {
    const beforeItemIdx = items.findIndex((c) =>
      cardMatchesMoveToken(c, beforeId),
    );
    if (beforeItemIdx >= 0) insertIdx = beforeItemIdx;
  } else if (columnPeers.length > 0) {
    const lastPeer = columnPeers[columnPeers.length - 1];
    const lastIdx = items.findIndex((c) => c === lastPeer);
    insertIdx = lastIdx >= 0 ? lastIdx + 1 : items.length;
  } else {
    insertIdx = items.length;
  }

  items.splice(insertIdx, 0, updatedCard);

  return {
    ...workspace,
    cards: {
      ...workspace.cards,
      items,
    },
  };
}

/** @param {object} workspace @param {object} cardItem */
export function insertOptimisticCard(workspace, cardItem) {
  if (!workspace?.cards) return workspace;
  const items = [...(workspace.cards.items ?? []), cardItem];
  return {
    ...workspace,
    cards: {
      ...workspace.cards,
      count: items.length,
      items,
    },
  };
}

const RISK_ORDER = { critical: 0, high: 1, medium: 2, low: 3 };

/** @type {{ value: "rank" | "updated" | "due" | "risk", label: string }[]} */
export const BOARD_CARD_SORT_OPTIONS = [
  { value: "rank", label: "Manual" },
  { value: "updated", label: "Updated" },
  { value: "due", label: "Due" },
  { value: "risk", label: "Priority" },
];

/**
 * @param {object[]} cards
 * @param {"rank" | "updated" | "due" | "risk"} sortMode
 */
export function sortColumnCards(cards, sortMode = "rank") {
  const list = [...(cards ?? [])];
  if (sortMode === "updated") {
    return list.sort((a, b) =>
      String(b?.membership?.updated_at ?? "").localeCompare(
        String(a?.membership?.updated_at ?? ""),
      ),
    );
  }
  if (sortMode === "due") {
    return list.sort((a, b) => {
      const da = String(a?.membership?.due_at ?? "").trim();
      const db = String(b?.membership?.due_at ?? "").trim();
      if (!da && !db) return 0;
      if (!da) return 1;
      if (!db) return -1;
      return da.localeCompare(db);
    });
  }
  if (sortMode === "risk") {
    return list.sort((a, b) => {
      const ra =
        RISK_ORDER[String(a?.membership?.risk ?? "medium").trim()] ?? 2;
      const rb =
        RISK_ORDER[String(b?.membership?.risk ?? "medium").trim()] ?? 2;
      if (ra !== rb) return ra - rb;
      return String(a?.membership?.rank ?? "").localeCompare(
        String(b?.membership?.rank ?? ""),
      );
    });
  }
  return list.sort((a, b) => {
    const ra = Number.parseInt(String(a?.membership?.rank ?? "0"), 10);
    const rb = Number.parseInt(String(b?.membership?.rank ?? "0"), 10);
    const safeA = Number.isFinite(ra) ? ra : 0;
    const safeB = Number.isFinite(rb) ? rb : 0;
    if (safeA !== safeB) return safeA - safeB;
    return String(a?.membership?.created_at ?? "").localeCompare(
      String(b?.membership?.created_at ?? ""),
    );
  });
}

/**
 * @param {object[]} items
 * @param {{ q?: string, risk?: string[], mineOnly?: boolean, dueFilter?: string }} filters
 * @param {string} currentActorId
 */
export function filterBoardCardItems(items, filters, currentActorId = "") {
  const q = String(filters?.q ?? "")
    .trim()
    .toLowerCase();
  const risks = new Set(
    (filters?.risk ?? []).map((r) => String(r).trim()).filter(Boolean),
  );
  const dueFilter = String(filters?.dueFilter ?? "").trim();
  const mineOnly = Boolean(filters?.mineOnly);
  const actorToken = currentActorId
    ? `actor:${currentActorId.replace(/^actor:/, "")}`
    : "";

  return (items ?? []).filter((item) => {
    const m = item?.membership ?? {};
    if (mineOnly && actorToken) {
      const assignees = (m.assignee_refs ?? []).map((r) =>
        String(r ?? "").trim(),
      );
      if (!assignees.includes(actorToken)) return false;
    }
    if (risks.size > 0 && !risks.has(String(m.risk ?? "medium").trim())) {
      return false;
    }
    if (q) {
      const hay = [
        m.title,
        m.summary,
        boardCardStableId(m),
        item?.backing?.thread?.title,
      ]
        .map((v) => String(v ?? "").toLowerCase())
        .join(" ");
      if (!hay.includes(q)) return false;
    }
    if (dueFilter) {
      const due = String(m.due_at ?? "").trim();
      if (!due) return dueFilter === "none";
      const dueMs = Date.parse(due);
      if (!Number.isFinite(dueMs)) return false;
      const now = Date.now();
      const week = 7 * 24 * 60 * 60 * 1000;
      if (dueFilter === "overdue" && dueMs >= now) return false;
      if (dueFilter === "soon" && (dueMs < now || dueMs > now + week)) {
        return false;
      }
      if (dueFilter === "none") return false;
    }
    return true;
  });
}

/** @param {string} boardId @returns {Record<string, boolean>} */
export function readCollapsedColumns(boardId) {
  if (typeof localStorage === "undefined") return {};
  try {
    const raw = localStorage.getItem(`anx-board-col-collapse:${boardId}`);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

/** @param {string} boardId @param {Record<string, boolean>} state */
export function writeCollapsedColumns(boardId, state) {
  if (typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(
      `anx-board-col-collapse:${boardId}`,
      JSON.stringify(state),
    );
  } catch {
    /* ignore quota */
  }
}

/**
 * Column cards excluding the card being dragged (peer-space ordering).
 * @param {object[]} columnCards
 * @param {string} draggingCardId
 */
export function columnCardsExcludingDrag(columnCards, draggingCardId) {
  const dragId = String(draggingCardId ?? "").trim();
  return (columnCards ?? []).filter(
    (card) => boardCardStableId(card?.membership) !== dragId,
  );
}

/**
 * @param {HTMLElement[]} slots
 * @param {number} fullInsertIndex
 * @param {string} draggingCardId
 */
export function peerInsertIndexFromSlotElements(
  slots,
  fullInsertIndex,
  draggingCardId,
) {
  const dragId = String(draggingCardId ?? "").trim();
  let peerIndex = 0;
  const bound = Math.max(0, Math.min(fullInsertIndex, slots.length));
  for (let i = 0; i < bound; i++) {
    const id = slots[i]?.getAttribute("data-card-id") ?? "";
    if (id !== dragId) peerIndex++;
  }
  return peerIndex;
}

/**
 * @param {object[]} columnCards
 * @param {number} fullInsertIndex
 */
export function dropBeforeCardIdAtFullInsert(columnCards, fullInsertIndex) {
  const cards = columnCards ?? [];
  const idx = Math.max(0, Math.min(fullInsertIndex, cards.length));
  if (idx >= cards.length) return "";
  return boardCardStableId(cards[idx]?.membership);
}

/**
 * @param {object[]} columnCards
 * @param {number} fullInsertIndex
 */
export function dropAfterCardIdAtFullInsert(columnCards, fullInsertIndex) {
  const cards = columnCards ?? [];
  if (fullInsertIndex < cards.length) return "";
  const last = cards[cards.length - 1];
  return last ? boardCardStableId(last.membership) : "";
}

/**
 * @param {HTMLElement} columnEl
 * @param {number} clientY
 * @returns {number}
 */
export function computeDropFullInsertIndex(columnEl, clientY) {
  const slots = [...columnEl.querySelectorAll("[data-board-card-slot]")];
  for (let i = 0; i < slots.length; i++) {
    const rect = slots[i].getBoundingClientRect();
    const mid = rect.top + rect.height / 2;
    if (clientY < mid) return i;
  }
  return slots.length;
}

/**
 * @param {HTMLElement} columnEl
 * @param {number} clientY
 * @param {string} draggingCardId
 * @returns {{ fullInsertIndex: number, peerInsertIndex: number }}
 */
export function computeDropPlacement(columnEl, clientY, draggingCardId) {
  const slots = [...columnEl.querySelectorAll("[data-board-card-slot]")];
  let fullInsertIndex = slots.length;
  for (let i = 0; i < slots.length; i++) {
    const rect = slots[i].getBoundingClientRect();
    const mid = rect.top + rect.height / 2;
    if (clientY < mid) {
      fullInsertIndex = i;
      break;
    }
  }
  const peerInsertIndex = peerInsertIndexFromSlotElements(
    slots,
    fullInsertIndex,
    draggingCardId,
  );
  return { fullInsertIndex, peerInsertIndex };
}

/**
 * Drop index from pointer Y relative to column card list (peer-space).
 * @param {HTMLElement} columnEl
 * @param {number} clientY
 * @param {string} draggingCardId
 */
export function computeDropIndex(columnEl, clientY, draggingCardId) {
  return computeDropPlacement(columnEl, clientY, draggingCardId)
    .peerInsertIndex;
}

/** @param {object} workspace @param {string} columnKey @param {string} excludeCardId */
export function columnPeerIdsExcluding(
  workspace,
  columnKey,
  excludeCardId,
  columnSchema,
) {
  const all = sortedColumnPeersStableIds(
    workspace?.cards,
    columnSchema,
    columnKey,
  );
  return all.filter((id) => id !== excludeCardId);
}
