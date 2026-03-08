import { coreClient } from "./coreClient";
import { computeStaleness } from "./threadFilters";

function createThreadDetailStore() {
  let state = {
    snapshot: null,
    snapshotLoading: false,
    snapshotError: "",
    commitments: [],
    commitmentsLoading: false,
    timeline: [],
    timelineLoading: false,
    timelineError: "",
    workOrders: [],
    workOrdersLoading: false,
    workOrdersError: "",
  };

  const getters = {
    get snapshot() {
      return state.snapshot;
    },
    get snapshotLoading() {
      return state.snapshotLoading;
    },
    get snapshotError() {
      return state.snapshotError;
    },
    get commitments() {
      return state.commitments;
    },
    get commitmentsLoading() {
      return state.commitmentsLoading;
    },
    get timeline() {
      return state.timeline;
    },
    get timelineLoading() {
      return state.timelineLoading;
    },
    get timelineError() {
      return state.timelineError;
    },
    get workOrders() {
      return state.workOrders;
    },
    get workOrdersLoading() {
      return state.workOrdersLoading;
    },
    get workOrdersError() {
      return state.workOrdersError;
    },
  };

  async function loadSnapshot(threadId) {
    state.snapshotLoading = true;
    state.snapshotError = "";
    try {
      state.snapshot = (await coreClient.getThread(threadId)).thread ?? null;
      return state.snapshot;
    } catch (e) {
      state.snapshotError = `Failed to load thread: ${e instanceof Error ? e.message : String(e)}`;
      state.snapshot = null;
      return null;
    } finally {
      state.snapshotLoading = false;
    }
  }

  async function loadCommitments(threadId) {
    state.commitmentsLoading = true;
    try {
      const response = await coreClient.listCommitments({
        thread_id: threadId,
        status: "open",
      });
      state.commitments = response.commitments ?? [];
    } catch {
      state.commitments = [];
    } finally {
      state.commitmentsLoading = false;
    }
  }

  async function loadTimeline(threadId) {
    state.timelineLoading = true;
    state.timelineError = "";
    try {
      state.timeline =
        (await coreClient.listThreadTimeline(threadId)).events ?? [];
    } catch (e) {
      state.timelineError = `Failed to load timeline: ${e instanceof Error ? e.message : String(e)}`;
      state.timeline = [];
    } finally {
      state.timelineLoading = false;
    }
  }

  async function loadWorkOrders(threadId) {
    state.workOrdersLoading = true;
    state.workOrdersError = "";
    try {
      const response = await coreClient.listArtifacts({
        kind: "work_order",
        thread_id: threadId,
      });
      state.workOrders = response.artifacts ?? [];
    } catch (error) {
      state.workOrdersError = `Failed to load work orders: ${error instanceof Error ? error.message : String(error)}`;
      state.workOrders = [];
    } finally {
      state.workOrdersLoading = false;
    }
  }

  async function refreshThreadDetail(threadId, flags = {}) {
    const {
      snapshot: refreshSnapshot = false,
      timeline: refreshTimeline = false,
      commitments: refreshCommitments = false,
      workOrders: refreshWorkOrders = false,
    } = flags;

    const promises = [];
    if (refreshSnapshot) promises.push(loadSnapshot(threadId));
    if (refreshTimeline) promises.push(loadTimeline(threadId));
    if (refreshCommitments) promises.push(loadCommitments(threadId));
    if (refreshWorkOrders) promises.push(loadWorkOrders(threadId));
    await Promise.all(promises);
  }

  async function fullRefresh(threadId) {
    await Promise.all([
      loadSnapshot(threadId),
      loadTimeline(threadId),
      loadCommitments(threadId),
      loadWorkOrders(threadId),
    ]);
  }

  function setSnapshot(value) {
    state.snapshot = value;
  }

  function setCommitments(value) {
    state.commitments = value;
  }

  function setTimeline(value) {
    state.timeline = value;
  }

  function setWorkOrders(value) {
    state.workOrders = value;
  }

  function getStaleness() {
    if (!state.snapshot) return null;
    return computeStaleness(state.snapshot);
  }

  function reset() {
    state = {
      snapshot: null,
      snapshotLoading: false,
      snapshotError: "",
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

  return {
    ...getters,
    loadSnapshot,
    loadCommitments,
    loadTimeline,
    loadWorkOrders,
    refreshThreadDetail,
    fullRefresh,
    setSnapshot,
    setCommitments,
    setTimeline,
    setWorkOrders,
    getStaleness,
    reset,
  };
}

export const threadDetailStore = createThreadDetailStore();
