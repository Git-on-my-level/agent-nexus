import { coreClient } from "./coreClient";
import { computeStaleness } from "./threadFilters";
import { get, writable } from "svelte/store";

function initialState() {
  return {
    workspace: null,
    snapshot: null,
    snapshotLoading: false,
    snapshotError: "",
    documents: [],
    documentsLoading: false,
    documentsError: "",
    commitments: [],
    commitmentsLoading: false,
    timeline: [],
    timelineLoading: false,
    timelineError: "",
    workOrders: [],
    workOrdersLoading: false,
    workOrdersError: "",
  };
}

function createThreadDetailStore() {
  const store = writable(initialState());
  const { subscribe, update, set } = store;
  const patchState = (patch) => update((state) => ({ ...state, ...patch }));
  let queuedRefreshFlags = null;
  let queuedRefreshThreadId = "";
  let queuedRefreshPromise = null;

  function mergeRefreshFlags(base = {}, next = {}) {
    const left = base ?? {};
    const right = next ?? {};
    return {
      workspace: Boolean(left.workspace || right.workspace),
      snapshot: Boolean(left.snapshot || right.snapshot),
      documents: Boolean(left.documents || right.documents),
      timeline: Boolean(left.timeline || right.timeline),
      commitments: Boolean(left.commitments || right.commitments),
      workOrders: Boolean(left.workOrders || right.workOrders),
    };
  }

  async function loadWorkspace(threadId, filters = {}) {
    const currentState = get(store);
    const hasWorkspaceData =
      currentState.workspace !== null ||
      currentState.snapshot !== null ||
      currentState.documents.length > 0 ||
      currentState.commitments.length > 0 ||
      currentState.timeline.length > 0;

    patchState({
      snapshotLoading: hasWorkspaceData ? currentState.snapshotLoading : true,
      snapshotError: hasWorkspaceData ? currentState.snapshotError : "",
      documentsLoading: hasWorkspaceData ? currentState.documentsLoading : true,
      documentsError: hasWorkspaceData ? currentState.documentsError : "",
      commitmentsLoading: hasWorkspaceData
        ? currentState.commitmentsLoading
        : true,
    });
    try {
      const workspace = await coreClient.getThreadWorkspace(threadId, filters);
      const context =
        workspace && typeof workspace.context === "object"
          ? workspace.context
          : {};
      patchState({
        workspace,
        snapshot: workspace?.thread ?? null,
        snapshotError: "",
        documents: Array.isArray(context.documents) ? context.documents : [],
        documentsError: "",
        commitments: Array.isArray(context.open_commitments)
          ? context.open_commitments
          : [],
        timeline: Array.isArray(context.recent_events)
          ? context.recent_events
          : [],
      });
      return workspace;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (hasWorkspaceData) {
        patchState({
          documentsError: `Failed to refresh workspace: ${message}`,
        });
      } else {
        patchState({
          workspace: null,
          snapshotError: `Failed to load workspace: ${message}`,
          snapshot: null,
          documentsError: `Failed to load workspace: ${message}`,
          documents: [],
          commitments: [],
          timeline: [],
        });
      }
      return null;
    } finally {
      patchState({
        snapshotLoading: false,
        documentsLoading: false,
        commitmentsLoading: false,
      });
    }
  }

  async function loadTimeline(threadId) {
    patchState({ timelineLoading: true, timelineError: "" });
    try {
      patchState({
        timeline: (await coreClient.listThreadTimeline(threadId)).events ?? [],
      });
    } catch (e) {
      patchState({
        timelineError: `Failed to load timeline: ${e instanceof Error ? e.message : String(e)}`,
        timeline: [],
      });
    } finally {
      patchState({ timelineLoading: false });
    }
  }

  async function loadWorkOrders(threadId) {
    patchState({ workOrdersLoading: true, workOrdersError: "" });
    try {
      const response = await coreClient.listArtifacts({
        kind: "work_order",
        thread_id: threadId,
      });
      patchState({ workOrders: response.artifacts ?? [] });
    } catch (error) {
      patchState({
        workOrdersError: `Failed to load work orders: ${error instanceof Error ? error.message : String(error)}`,
        workOrders: [],
      });
    } finally {
      patchState({ workOrdersLoading: false });
    }
  }

  async function refreshThreadDetail(threadId, flags = {}) {
    const {
      workspace: refreshWorkspace = false,
      snapshot: refreshSnapshot = false,
      documents: refreshDocuments = false,
      timeline: refreshTimeline = false,
      commitments: refreshCommitments = false,
      workOrders: refreshWorkOrders = false,
    } = flags;

    const promises = [];
    if (
      refreshWorkspace ||
      refreshSnapshot ||
      refreshDocuments ||
      refreshCommitments
    ) {
      promises.push(loadWorkspace(threadId));
    }
    if (refreshTimeline) promises.push(loadTimeline(threadId));
    if (refreshWorkOrders) promises.push(loadWorkOrders(threadId));
    await Promise.all(promises);
  }

  async function queueRefreshThreadDetail(threadId, flags = {}) {
    if (!threadId) return;

    if (queuedRefreshThreadId && queuedRefreshThreadId !== threadId) {
      queuedRefreshFlags = null;
      queuedRefreshPromise = null;
    }

    queuedRefreshThreadId = threadId;
    queuedRefreshFlags = mergeRefreshFlags(queuedRefreshFlags, flags);

    if (queuedRefreshPromise) {
      return queuedRefreshPromise;
    }

    queuedRefreshPromise = (async () => {
      while (queuedRefreshFlags) {
        const nextFlags = queuedRefreshFlags;
        queuedRefreshFlags = null;
        await refreshThreadDetail(queuedRefreshThreadId, nextFlags);
      }
    })().finally(() => {
      queuedRefreshPromise = null;
      queuedRefreshThreadId = "";
    });

    return queuedRefreshPromise;
  }

  async function fullRefresh(threadId) {
    await Promise.all([loadWorkspace(threadId), loadWorkOrders(threadId)]);
  }

  function setSnapshot(value) {
    patchState({ snapshot: value });
  }

  function setCommitments(value) {
    patchState({ commitments: value });
  }

  function setDocuments(value) {
    patchState({ documents: value });
  }

  function setTimeline(value) {
    patchState({ timeline: value });
  }

  function setWorkOrders(value) {
    patchState({ workOrders: value });
  }

  function getStaleness(snapshot) {
    const value = snapshot ?? get(store).snapshot;
    if (!value) return null;
    return computeStaleness(value);
  }

  function reset() {
    set(initialState());
  }

  return {
    subscribe,
    loadWorkspace,
    loadTimeline,
    loadWorkOrders,
    refreshThreadDetail,
    queueRefreshThreadDetail,
    fullRefresh,
    setSnapshot,
    setDocuments,
    setCommitments,
    setTimeline,
    setWorkOrders,
    getStaleness,
    reset,
  };
}

export const threadDetailStore = createThreadDetailStore();
