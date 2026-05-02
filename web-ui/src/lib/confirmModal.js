/**
 * Shared defaults for destructive / bulk confirmation flows (ConfirmModal).
 * Pages still own copy derivation; this keeps empty shape consistent.
 */

/** @typedef {{ open: boolean, action: string, entityId?: string, bulkIds?: string[] | null }} ConfirmModalModel */

/** @returns {ConfirmModalModel} */
export function emptyConfirmModal() {
  return {
    open: false,
    action: "",
    entityId: "",
    bulkIds: null,
  };
}

/**
 * @param {ConfirmModalModel} state
 * @param {Partial<ConfirmModalModel>} patch
 */
export function openConfirmModal(state, patch) {
  return {
    ...state,
    ...patch,
    open: true,
  };
}

/** @param {ConfirmModalModel} state */
export function closeConfirmModal(state) {
  return {
    ...state,
    open: false,
    action: "",
    entityId: "",
    bulkIds: null,
  };
}

/** Timeline tab: archive/trash events (optional bulk). */
export function emptyTimelineConfirmModal() {
  return {
    open: false,
    action: "",
    eventId: "",
    eventRawType: "",
    bulkIds: null,
  };
}

/** Messages tab: single-event archive/trash. */
export function emptyMessageEventConfirmModal() {
  return { open: false, action: "", eventId: "" };
}
