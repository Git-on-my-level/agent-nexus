import {
  cardIdFromWork,
  sortWorkBoardItems,
  workKey,
} from "./pm/presentation.js";

export const DRAG_PLACEHOLDER_KEY = "__drag-placeholder";
export const DRAG_THRESHOLD_PX = 6;

/**
 * Remaining cards plus a hole at the pointer so neighbors shift while a
 * card is in the air. Drop uses the same index so the card lands in the hole.
 * @param {object[]} items
 * @param {string} dragKey
 * @param {string} groupKey
 * @param {{ phase: string, index: number } | null} hover
 */
export function columnSlots(items, dragKey, groupKey, hover) {
  const slots = (Array.isArray(items) ? items : [])
    .filter((work) => !dragKey || workKey(work) !== dragKey)
    .map((work) => ({ key: workKey(work), work }));
  if (!dragKey || !hover || hover.phase !== groupKey) return slots;
  const index = Math.max(
    0,
    Math.min(hover.index ?? slots.length, slots.length),
  );
  slots.splice(index, 0, { key: DRAG_PLACEHOLDER_KEY, placeholder: true });
  return slots;
}

/**
 * Column under the pointer, using geometry so a floating card cannot steal
 * hit-testing. Falls back to the column whose x-range contains the pointer
 * when the pointer is over the header or below the list.
 * @param {number} x
 * @param {number} y
 * @param {ParentNode} [root]
 * @returns {HTMLElement | null}
 */
export function columnAtPoint(x, y, root = document) {
  const columns = /** @type {HTMLElement[]} */ ([
    ...root.querySelectorAll("[data-work-phase-column]"),
  ]);
  let fallback = /** @type {HTMLElement | null} */ (null);
  for (const column of columns) {
    const rect = column.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) continue;
    if (x < rect.left || x > rect.right) continue;
    if (y >= rect.top && y <= rect.bottom) return column;
    fallback = column;
  }
  return fallback;
}

/**
 * Insert index among cards that are still sitting in the column (the
 * floating card and its hole are ignored so the hole does not chase itself).
 * @param {HTMLElement} columnEl
 * @param {number} clientY
 * @param {string} [dragKey]
 */
export function insertIndexAtY(columnEl, clientY, dragKey = "") {
  const slots = [...columnEl.querySelectorAll("[data-work-slot]")].filter(
    (el) =>
      !el.hasAttribute("data-placeholder") &&
      (!dragKey || el.getAttribute("data-work-ref") !== dragKey),
  );
  for (let i = 0; i < slots.length; i++) {
    const rect = slots[i].getBoundingClientRect();
    if (clientY < rect.top + rect.height / 2) return i;
  }
  return slots.length;
}

/**
 * Remaining cards in a phase, in the same order the board paints
 * (`board_ref`, then `rank`). Drop indexes are in this space, not work.list
 * recency.
 * @param {object[]} records
 * @param {string} phase
 * @param {string} [excludeKey]
 */
export function boardPhasePeers(records, phase, excludeKey = "") {
  return sortWorkBoardItems(
    (Array.isArray(records) ? records : []).filter(
      (item) =>
        workKey(item) !== excludeKey && (item.phase || "unknown") === phase,
    ),
  );
}

/**
 * Put `work` at `index` among records that share `phase`, matching the hole
 * shown while dragging. Other phases keep their relative order.
 * @param {object[]} records
 * @param {object} work
 * @param {string} phase
 * @param {number} index
 */
export function placeWorkInPhase(records, work, phase, index) {
  const key = workKey(work);
  const rest = (Array.isArray(records) ? records : []).filter(
    (item) => workKey(item) !== key,
  );
  const moved = { ...work, phase };
  const boardRef = String(work?.board_ref ?? "").trim();
  const peers = boardPhasePeers(rest, phase);
  const insertAt = Math.max(
    0,
    Math.min(Number.isFinite(index) ? index : peers.length, peers.length),
  );
  const nextPeers = applyLocalBoardRank(
    [...peers.slice(0, insertAt), moved, ...peers.slice(insertAt)],
    phase,
    boardRef,
  );
  const out = [];
  let inserted = false;
  for (const item of rest) {
    if ((item.phase || "unknown") === phase) {
      if (!inserted) {
        out.push(...nextPeers);
        inserted = true;
      }
      continue;
    }
    out.push(item);
  }
  if (!inserted) out.push(...nextPeers);
  return out;
}

/**
 * Neighbor ref for cards.move at the hole index. Empty means append.
 * Walks remaining cards from that index and skips other boards.
 * @param {object} work
 * @param {object[]} peers remaining cards in the target phase (hole order)
 * @param {number} insertIndex
 */
export function beforeCardRefForInsert(work, peers, insertIndex) {
  const remaining = Array.isArray(peers) ? peers : [];
  const board = String(work?.board_ref ?? "").trim();
  const start = Math.max(
    0,
    Number.isInteger(insertIndex) ? insertIndex : remaining.length,
  );
  if (start >= remaining.length) return "";
  for (let i = start; i < remaining.length; i += 1) {
    const peerBoard = String(remaining[i]?.board_ref ?? "").trim();
    if (board && peerBoard && peerBoard !== board) continue;
    return cardIdFromWork(remaining[i]);
  }
  return "";
}

function applyLocalBoardRank(records, phase, boardRef) {
  const peers = records.filter(
    (item) =>
      (item.phase || "unknown") === phase &&
      (!boardRef || String(item.board_ref ?? "").trim() === boardRef),
  );
  const ranks = new Map(
    peers.map((item, i) => [workKey(item), String(i + 1).padStart(19, "0")]),
  );
  return records.map((item) => {
    const rank = ranks.get(workKey(item));
    return rank ? { ...item, rank } : item;
  });
}

export function flipDuration(distance) {
  if (
    typeof window !== "undefined" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  ) {
    return 0;
  }
  const len = Number.isFinite(distance) ? distance : 80;
  return Math.min(180, Math.max(80, Math.sqrt(len) * 12));
}
