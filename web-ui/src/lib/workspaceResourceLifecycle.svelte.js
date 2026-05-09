/**
 * Shared workspace resource lifecycle controller (runes: $state processed by Svelte).
 * Keeps list routes responsible for rendering and resource-specific predicates while
 * centralizing confirmation state, busy flags, bulk sequencing, reloads, and errors.
 * @eslint no-undef: Svelte 5 compiler provides $state in `.svelte.js`.
 */
/* eslint-disable no-undef -- Svelte 5 runes */
import { emptyConfirmModal } from "$lib/confirmModal.js";

/** @internal */
function normalizeId(id) {
  return String(id ?? "").trim();
}

/** @param {Iterable<unknown>} ids */
export function normalizeResourceIds(ids) {
  return [...ids].map((id) => normalizeId(id)).filter(Boolean);
}

/**
 * @typedef {"archive"|"unarchive"|"trash"} WorkspaceLifecycleAction
 *
 * @typedef {object} WorkspaceResourceLifecycleOptions
 * @property {string} resourceSingular
 * @property {string} resourcePlural
 * @property {() => unknown[]} selectedItems
 * @property {(item: unknown) => string} idFor
 * @property {(item: unknown) => boolean} isArchived
 * @property {(item: unknown) => boolean} isTrashed
 * @property {{ archive?: (id: string) => Promise<unknown>, unarchive?: (id: string) => Promise<unknown>, trash?: (id: string) => Promise<unknown> }} actions
 * @property {() => Promise<unknown>} reload
 * @property {() => void} clearSelection
 * @property {(message: string) => void} setError
 */

/**
 * @param {WorkspaceResourceLifecycleOptions} options
 */
export function createWorkspaceResourceLifecycleController(options) {
  let archiveBusyId = $state("");
  let trashBusyId = $state("");
  let bulkBusy = $state(false);
  let confirmModal = $state(emptyConfirmModal());

  /** @param {WorkspaceLifecycleAction} action */
  function actionRunner(action) {
    return options.actions[action];
  }

  /** @param {WorkspaceLifecycleAction} action */
  function actionLabel(action) {
    if (action === "unarchive") return "Unarchive";
    if (action === "trash") return "Trash";
    return "Archive";
  }

  /** @param {WorkspaceLifecycleAction} action */
  function setSingleBusy(action, id) {
    if (action === "archive") archiveBusyId = id;
    if (action === "trash") trashBusyId = id;
  }

  /** @param {WorkspaceLifecycleAction} action */
  function clearSingleBusy(action) {
    if (action === "archive") archiveBusyId = "";
    if (action === "trash") trashBusyId = "";
  }

  /** @param {(item: unknown) => boolean} predicate */
  function selectedIdsWhere(predicate) {
    return options
      .selectedItems()
      .filter(predicate)
      .map((item) => options.idFor(item))
      .map((id) => normalizeId(id))
      .filter(Boolean);
  }

  function idsForArchive() {
    return selectedIdsWhere(
      (item) => !options.isArchived(item) && !options.isTrashed(item),
    );
  }

  function idsForUnarchive() {
    return selectedIdsWhere(
      (item) => options.isArchived(item) && !options.isTrashed(item),
    );
  }

  function idsForTrash() {
    return selectedIdsWhere((item) => !options.isTrashed(item));
  }

  function canArchive() {
    return idsForArchive().length > 0;
  }

  function canUnarchive() {
    return idsForUnarchive().length > 0;
  }

  function canTrash() {
    return idsForTrash().length > 0;
  }

  /**
   * @param {WorkspaceLifecycleAction} action
   * @param {unknown} id
   */
  async function runSingle(action, id) {
    const runner = actionRunner(action);
    const normalized = normalizeId(id);
    if (!runner || !normalized || archiveBusyId || trashBusyId || bulkBusy) {
      return;
    }
    setSingleBusy(action, normalized);
    options.setError("");
    try {
      await runner(normalized);
      confirmModal = emptyConfirmModal();
      await options.reload();
    } catch (e) {
      options.setError(
        `${actionLabel(action)} failed: ${e instanceof Error ? e.message : String(e)}`,
      );
    } finally {
      clearSingleBusy(action);
    }
  }

  /**
   * @param {WorkspaceLifecycleAction} action
   * @param {Iterable<unknown>} ids
   */
  async function runBulk(action, ids) {
    const runner = actionRunner(action);
    const list = normalizeResourceIds(ids);
    if (!runner || !list.length || bulkBusy) return;
    bulkBusy = true;
    options.setError("");
    try {
      for (const id of list) {
        await runner(id);
      }
      options.clearSelection();
      confirmModal = emptyConfirmModal();
      await options.reload();
    } catch (e) {
      options.setError(
        `${actionLabel(action)} failed: ${e instanceof Error ? e.message : String(e)}`,
      );
    } finally {
      bulkBusy = false;
    }
  }

  /** @param {WorkspaceLifecycleAction} action */
  function openBulkConfirm(action, ids) {
    const list = normalizeResourceIds(ids);
    if (!list.length) return;
    confirmModal = {
      open: true,
      action,
      entityId: "",
      bulkIds: list,
    };
  }

  /** @param {WorkspaceLifecycleAction} action */
  function openSingleConfirm(action, id) {
    const entityId = normalizeId(id);
    if (!entityId) return;
    confirmModal = {
      open: true,
      action,
      entityId,
      bulkIds: null,
    };
  }

  function closeConfirm() {
    confirmModal = emptyConfirmModal();
  }

  function handleConfirm() {
    const bulkIds = confirmModal.bulkIds;
    const id = confirmModal.entityId;
    const action = /** @type {WorkspaceLifecycleAction} */ (
      confirmModal.action
    );
    confirmModal = emptyConfirmModal();
    if (bulkIds && bulkIds.length > 0) {
      void runBulk(action, bulkIds);
      return;
    }
    void runSingle(action, id);
  }

  function confirmBulkCount() {
    return confirmModal.bulkIds?.length ?? 0;
  }

  function confirmIsBulk() {
    return confirmBulkCount() > 0;
  }

  function confirmTitle() {
    const count = confirmBulkCount();
    const isBulk = count > 0;
    if (confirmModal.action === "trash") {
      return isBulk
        ? `Move ${count} ${options.resourcePlural} to trash`
        : "Move to trash";
    }
    return isBulk
      ? `Archive ${count} ${options.resourcePlural}`
      : `Archive ${options.resourceSingular}`;
  }

  function confirmMessage() {
    const count = confirmBulkCount();
    const isBulk = count > 0;
    if (confirmModal.action === "trash") {
      return isBulk
        ? `These ${options.resourcePlural} (${count}) will be moved to trash. You can restore them later.`
        : `This ${options.resourceSingular} will be moved to trash. You can restore it later.`;
    }
    return isBulk
      ? `These ${options.resourcePlural} (${count}) will be hidden from default views. You can unarchive them later.`
      : `This ${options.resourceSingular} will be hidden from default views. You can unarchive it later.`;
  }

  function confirmBusy() {
    const isBulk = confirmIsBulk();
    if (confirmModal.action === "trash") {
      return Boolean(trashBusyId) || (isBulk && bulkBusy);
    }
    return Boolean(archiveBusyId) || (isBulk && bulkBusy);
  }

  return {
    get archiveBusyId() {
      return archiveBusyId;
    },
    get trashBusyId() {
      return trashBusyId;
    },
    get bulkBusy() {
      return bulkBusy;
    },
    get confirmModal() {
      return confirmModal;
    },
    idsForArchive,
    idsForUnarchive,
    idsForTrash,
    canArchive,
    canUnarchive,
    canTrash,
    runSingle,
    runBulk,
    openBulkConfirm,
    openSingleConfirm,
    closeConfirm,
    handleConfirm,
    confirmBulkCount,
    confirmIsBulk,
    confirmTitle,
    confirmMessage,
    confirmBusy,
  };
}

/**
 * @typedef {object} WorkspaceBulkActionOptions
 * @property {() => Promise<unknown>} reload
 * @property {() => void} clearSelection
 * @property {(message: string) => void} setError
 */

/** @param {WorkspaceBulkActionOptions} options */
export function createWorkspaceBulkActionController(options) {
  let busy = $state(false);

  /**
   * @param {Iterable<unknown>} ids
   * @param {(id: string) => Promise<unknown>} action
   * @param {{ errorPrefix: string, afterSuccess?: () => void }} config
   */
  async function run(ids, action, config) {
    const list = normalizeResourceIds(ids);
    if (!list.length || busy) return;
    busy = true;
    options.setError("");
    try {
      for (const id of list) {
        await action(id);
      }
      config.afterSuccess?.();
      options.clearSelection();
      await options.reload();
    } catch (e) {
      options.setError(
        `${config.errorPrefix}: ${e instanceof Error ? e.message : String(e)}`,
      );
    } finally {
      busy = false;
    }
  }

  return {
    get busy() {
      return busy;
    },
    run,
  };
}

/**
 * @typedef {"artifacts"|"documents"|"topics"|"boards"|"cards"} TrashResourceType
 *
 * @typedef {object} WorkspaceTrashResourceActionsOptions
 * @property {*} coreClient
 * @property {() => Record<string, unknown>} [actorBody]
 */

/** @param {WorkspaceTrashResourceActionsOptions} options */
export function createWorkspaceTrashResourceActions(options) {
  const actorBody = options.actorBody ?? (() => ({}));

  /** @param {TrashResourceType} type */
  function restore(type, id) {
    switch (type) {
      case "artifacts":
        return options.coreClient.restoreArtifact(id, {});
      case "documents":
        return options.coreClient.restoreDocument(id, {});
      case "topics":
        return options.coreClient.restoreTopic(id, {});
      case "boards":
        return options.coreClient.restoreBoard(id, {});
      case "cards":
        return options.coreClient.restoreCard(id, {});
      default:
        throw new Error("Unsupported restore type");
    }
  }

  /** @param {TrashResourceType} type */
  function purge(type, id) {
    const body = actorBody();
    switch (type) {
      case "artifacts":
        return options.coreClient.purgeArtifact(id, body);
      case "documents":
        return options.coreClient.purgeDocument(id, body);
      case "boards":
        return options.coreClient.purgeBoard(id, body);
      case "cards":
        return options.coreClient.purgeCard(id, body);
      default:
        throw new Error("Unsupported purge type");
    }
  }

  return { restore, purge };
}
