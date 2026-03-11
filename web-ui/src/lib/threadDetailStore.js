import { coreClient } from "./coreClient";
import { computeStaleness } from "./threadFilters";
import { get, writable } from "svelte/store";

function initialState() {
  return {
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

  async function loadSnapshot(threadId) {
    patchState({ snapshotLoading: true, snapshotError: "" });
    try {
      const snapshot = (await coreClient.getThread(threadId)).thread ?? null;
      patchState({ snapshot });
      return snapshot;
    } catch (e) {
      patchState({
        snapshotError: `Failed to load thread: ${e instanceof Error ? e.message : String(e)}`,
        snapshot: null,
      });
      return null;
    } finally {
      patchState({ snapshotLoading: false });
    }
  }

  async function loadCommitments(threadId) {
    patchState({ commitmentsLoading: true });
    try {
      const response = await coreClient.listCommitments({
        thread_id: threadId,
        status: "open",
      });
      patchState({ commitments: response.commitments ?? [] });
    } catch {
      patchState({ commitments: [] });
    } finally {
      patchState({ commitmentsLoading: false });
    }
  }

  async function loadDocuments(threadId) {
    patchState({ documentsLoading: true, documentsError: "" });
    try {
      const response = await coreClient.listDocuments({ thread_id: threadId });
      patchState({ documents: response.documents ?? [] });
    } catch (error) {
      patchState({
        documentsError: `Failed to load documents: ${error instanceof Error ? error.message : String(error)}`,
        documents: [],
      });
    } finally {
      patchState({ documentsLoading: false });
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
      snapshot: refreshSnapshot = false,
      documents: refreshDocuments = false,
      timeline: refreshTimeline = false,
      commitments: refreshCommitments = false,
      workOrders: refreshWorkOrders = false,
    } = flags;

    const promises = [];
    if (refreshSnapshot) promises.push(loadSnapshot(threadId));
    if (refreshDocuments) promises.push(loadDocuments(threadId));
    if (refreshTimeline) promises.push(loadTimeline(threadId));
    if (refreshCommitments) promises.push(loadCommitments(threadId));
    if (refreshWorkOrders) promises.push(loadWorkOrders(threadId));
    await Promise.all(promises);
  }

  async function fullRefresh(threadId) {
    await Promise.all([
      loadSnapshot(threadId),
      loadDocuments(threadId),
      loadTimeline(threadId),
      loadCommitments(threadId),
      loadWorkOrders(threadId),
    ]);
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
    loadSnapshot,
    loadDocuments,
    loadCommitments,
    loadTimeline,
    loadWorkOrders,
    refreshThreadDetail,
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
