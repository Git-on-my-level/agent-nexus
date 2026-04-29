/**
 * Shared workspace list selection helpers (runes: $state/$effect processed by Svelte).
 * @eslint no-undef: Svelte 5 compiler provides $state/$effect in `.svelte.js`.
 */
/* eslint-disable no-undef -- Svelte 5 runes */
import { goto } from "$app/navigation";

/** @internal @param {*} id */
function coerceId(id) {
  const s = String(id ?? "").trim();
  return s;
}

/**
 * @typedef {object} WorkspaceListSelectionOptions
 * @property {() => boolean} [bulkBusy] When true, row interactions are suppressed.
 * @property {() => boolean} [when] Must be true (e.g. route surface is "topics") for row interaction.
 */

/**
 * @param {WorkspaceListSelectionOptions} [options]
 */
export function createWorkspaceListSelection(options = {}) {
  const bulkBusy = options.bulkBusy ?? (() => false);
  const when = options.when ?? (() => true);

  /** @type {boolean} */
  let selectMode = $state(false);
  /** @type {Set<string>} */
  let selectedIds = $state(new Set());
  /** @type {number | null} */
  let selectionAnchorIndex = $state(null);

  /** @internal */
  function interactionEnabled() {
    return Boolean(selectMode && when() && !bulkBusy());
  }

  /** Build from `$effect` when backing rows reload */
  /** @param {Iterable<string>} validVisibleIds */
  function reconcileSelectionWithIds(validVisibleIds) {
    const valid = new Set(
      [...validVisibleIds].map((id) => coerceId(id)).filter(Boolean),
    );
    const next = new Set([...selectedIds].filter((id) => valid.has(id)));
    if (next.size !== selectedIds.size) {
      selectedIds = next;
    }
  }

  function clearSelection() {
    selectedIds = new Set();
  }

  /** @param {Iterable<string>} allVisibleIds Ordered list of ids matching rows */
  function selectAllFromVisibleIds(allVisibleIds) {
    selectedIds = new Set(
      [...allVisibleIds].map((id) => coerceId(id)).filter(Boolean),
    );
  }

  function toggleSelectMode() {
    selectMode = !selectMode;
    if (!selectMode) {
      clearSelection();
      selectionAnchorIndex = null;
    }
  }

  /** @param {(rowIndex: number) => string} idAtVisibleIndex */
  function applyRangeInclusive(fromIdx, toIdx, idAtVisibleIndex) {
    const lo = Math.min(fromIdx, toIdx);
    const hi = Math.max(fromIdx, toIdx);
    const next = new Set(selectedIds);
    for (let i = lo; i <= hi; i++) {
      const id = coerceId(idAtVisibleIndex(i));
      if (id) next.add(id);
    }
    selectedIds = next;
  }

  /**
   * @param {MouseEvent} e
   * @param {number} index Row index into the flattened visible list (0 … rowsLength-1)
   * @param {number} rowsLength
   * @param {(i: number) => string | undefined | null} idAtVisibleIndex
   * @param {(i: number) => string} hrefAtVisibleIndex
   */
  function handleRowMouseEvent(
    e,
    index,
    rowsLength,
    idAtVisibleIndex,
    hrefAtVisibleIndex,
  ) {
    if (!interactionEnabled()) return;
    const href = hrefAtVisibleIndex(index);
    const ce = /** @type {MouseEvent & { detail?: number }} */ (e);
    if ((ce.detail ?? 1) >= 2) {
      void goto(href);
      return;
    }
    if (e.shiftKey && selectionAnchorIndex !== null) {
      applyRangeInclusive(selectionAnchorIndex, index, idAtVisibleIndex);
      return;
    }
    const id = coerceId(idAtVisibleIndex(index));
    if (!id) return;
    const next = new Set(selectedIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedIds = next;
    selectionAnchorIndex = index;
  }

  /**
   * @param {KeyboardEvent} e
   * @param {number} index
   * @param {(i: number) => string | undefined | null} idAtVisibleIndex
   */
  function handleRowKeyboardEvent(e, index, idAtVisibleIndex) {
    if (!interactionEnabled()) return;
    if (e.key !== " " && e.key !== "Enter") return;
    e.preventDefault();
    if (e.shiftKey && selectionAnchorIndex !== null) {
      applyRangeInclusive(selectionAnchorIndex, index, idAtVisibleIndex);
      return;
    }
    const id = coerceId(idAtVisibleIndex(index));
    if (!id) return;
    const next = new Set(selectedIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedIds = next;
    selectionAnchorIndex = index;
  }

  $effect(() => {
    if (!selectMode) return;
    /** @param {KeyboardEvent} ev */
    function onEsc(ev) {
      if (ev.key !== "Escape") return;
      selectMode = false;
      clearSelection();
      selectionAnchorIndex = null;
    }
    document.addEventListener("keydown", onEsc);
    return () => document.removeEventListener("keydown", onEsc);
  });

  /** Optional: Topics route exposes both /topics and /threads — clear selection without toggling UI */
  function exitSelectionMode() {
    selectMode = false;
    clearSelection();
    selectionAnchorIndex = null;
  }

  return {
    get selectMode() {
      return selectMode;
    },
    get selectedIds() {
      return selectedIds;
    },
    get selectionAnchorIndex() {
      return selectionAnchorIndex;
    },
    reconcileSelectionWithIds,
    clearSelection,
    selectAllFromVisibleIds,
    toggleSelectMode,
    exitSelectionMode,
    handleRowMouseEvent,
    handleRowKeyboardEvent,
    applyRangeInclusive,
  };
}
