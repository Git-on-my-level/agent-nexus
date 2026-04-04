import { coreClient } from "./coreClient";
import { computeStaleness } from "./threadFilters";
import { get, writable } from "svelte/store";

function parseTypedRefId(ref, prefix) {
  const s = String(ref ?? "").trim();
  const p = `${prefix}:`;
  if (s.startsWith(p)) return s.slice(p.length).trim();
  return "";
}

function primaryThreadFromTopicWorkspace(workspace) {
  const topic = workspace?.topic;
  const primaryId = String(topic?.thread_id ?? "").trim();
  const threads = Array.isArray(workspace?.threads) ? workspace.threads : [];
  if (primaryId) {
    const found = threads.find((t) => String(t?.id) === primaryId);
    if (found) return found;
  }
  return threads[0] ?? null;
}

function deriveBoardPanelsFromTopicWorkspace(workspace, topicId) {
  const boards = Array.isArray(workspace?.boards) ? workspace.boards : [];
  const cards = Array.isArray(workspace?.cards) ? workspace.cards : [];
  const primaryThread = primaryThreadFromTopicWorkspace(workspace);
  const primaryThreadId = String(primaryThread?.id ?? "").trim();
  const topicIdStr = String(topicId ?? "").trim();

  const ownedBoards = boards
    .filter((b) => {
      const fromRef = parseTypedRefId(b?.primary_topic_ref, "topic");
      if (topicIdStr && fromRef === topicIdStr) return true;
      const raw = String(b?.primary_topic_ref ?? "").trim();
      return topicIdStr && raw === `topic:${topicIdStr}`;
    })
    .map((b) => ({
      id: b.id,
      title: b.title,
      status: b.status,
      card_count: cards.filter((c) => String(c?.board_id) === String(b.id))
        .length,
      updated_at: b.updated_at,
    }));

  const boardById = new Map(boards.map((b) => [String(b.id), b]));
  const boardMemberships = [];
  for (const card of cards) {
    const cid = String(card?.thread_id ?? "").trim();
    if (!primaryThreadId || cid !== primaryThreadId) continue;
    const boardId =
      String(card?.board_id ?? "").trim() ||
      parseTypedRefId(card?.board_ref, "board");
    if (!boardId) continue;
    const board = boardById.get(boardId) ?? { id: boardId, title: boardId };
    boardMemberships.push({ board, card });
  }

  return { ownedBoards, boardMemberships };
}

function initialState() {
  return {
    workspace: null,
    snapshot: null,
    snapshotLoading: false,
    snapshotError: "",
    documents: [],
    documentsLoading: false,
    documentsError: "",
    boardMemberships: [],
    ownedBoards: [],
    timelineThreadId: "",
    timeline: [],
    timelineLoading: false,
    timelineError: "",
    workOrders: [],
    workOrdersLoading: false,
    /** When true, workspace/timeline loads use topic-scoped APIs for the route id. */
    detailAsTopic: false,
  };
}

function createThreadDetailStore() {
  const store = writable(initialState());
  const { subscribe, update, set } = store;
  const patchState = (patch) => update((state) => ({ ...state, ...patch }));
  let queuedRefreshFlags = null;
  let queuedRefreshThreadId = "";
  let queuedRefreshPromise = null;
  let timelineRequestSeq = 0;
  /** Controls thread vs topic API for workspace/timeline refresh coalescing. */
  let detailAsTopic = false;

  function mergeRefreshFlags(base, next) {
    const left = base ?? {};
    const right = next ?? {};
    return {
      workspace: Boolean(left.workspace || right.workspace),
      snapshot: Boolean(left.snapshot || right.snapshot),
      documents: Boolean(left.documents || right.documents),
      timeline: Boolean(left.timeline || right.timeline),
      workOrders: Boolean(left.workOrders || right.workOrders),
    };
  }

  function timelineScopeIdForRoute(routeId) {
    return String(routeId ?? "").trim();
  }

  async function loadWorkspace(routeId, opts = {}) {
    if (typeof opts.asTopic === "boolean") {
      detailAsTopic = opts.asTopic;
      patchState({ detailAsTopic });
    }

    const threadId = timelineScopeIdForRoute(routeId);
    const asTopic = detailAsTopic;
    const currentState = get(store);
    const hasWorkspaceData =
      currentState.workspace !== null ||
      currentState.snapshot !== null ||
      currentState.documents.length > 0 ||
      currentState.timeline.length > 0;

    patchState({
      snapshotLoading: hasWorkspaceData ? currentState.snapshotLoading : true,
      snapshotError: hasWorkspaceData ? currentState.snapshotError : "",
      documentsLoading: hasWorkspaceData ? currentState.documentsLoading : true,
      documentsError: hasWorkspaceData ? currentState.documentsError : "",
    });
    try {
      let workspace;
      let snapshot;
      let documents = [];
      let boardMemberships = [];
      let ownedBoards = [];

      if (asTopic) {
        workspace = await coreClient.getTopicWorkspace(threadId, {});
        const primaryThread = primaryThreadFromTopicWorkspace(workspace);
        snapshot = primaryThread;
        documents = Array.isArray(workspace?.documents)
          ? workspace.documents
          : [];
        const derived = deriveBoardPanelsFromTopicWorkspace(
          workspace,
          threadId,
        );
        boardMemberships = derived.boardMemberships;
        ownedBoards = derived.ownedBoards;
      } else {
        workspace = await coreClient.getThreadWorkspace(threadId, {});
        const context =
          workspace && typeof workspace.context === "object"
            ? workspace.context
            : {};
        const boardMembershipsData =
          workspace && typeof workspace.board_memberships === "object"
            ? workspace.board_memberships
            : {};
        const ownedBoardsData =
          workspace && typeof workspace.owned_boards === "object"
            ? workspace.owned_boards
            : {};
        snapshot = workspace?.thread ?? null;
        documents = Array.isArray(context.documents) ? context.documents : [];
        boardMemberships = Array.isArray(boardMembershipsData.items)
          ? boardMembershipsData.items
          : [];
        ownedBoards = Array.isArray(ownedBoardsData.items)
          ? ownedBoardsData.items
          : [];
      }

      const latestState = get(store);
      const canReuseTimeline =
        latestState.timelineThreadId === threadId &&
        latestState.timeline.length > 0;

      let timelinePatch;
      if (asTopic) {
        timelinePatch = canReuseTimeline
          ? {}
          : { timeline: [], timelineThreadId: "" };
      } else {
        const context =
          workspace && typeof workspace.context === "object"
            ? workspace.context
            : {};
        timelinePatch = canReuseTimeline
          ? {}
          : {
              timeline: Array.isArray(context.recent_events)
                ? context.recent_events
                : [],
              timelineThreadId: Array.isArray(context.recent_events)
                ? threadId
                : "",
            };
      }

      patchState({
        workspace,
        snapshot,
        snapshotError: "",
        documents,
        documentsError: "",
        boardMemberships,
        ownedBoards,
        ...timelinePatch,
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
          boardMemberships: [],
          ownedBoards: [],
          timeline: [],
          timelineThreadId: "",
        });
      }
      return null;
    } finally {
      patchState({
        snapshotLoading: false,
        documentsLoading: false,
      });
    }
  }

  function resolveTimelineAsTopic(opts = {}) {
    if (typeof opts.asTopic === "boolean") return opts.asTopic;
    return detailAsTopic;
  }

  async function loadTimeline(routeId, opts = {}) {
    const requestSeq = ++timelineRequestSeq;
    const threadId = timelineScopeIdForRoute(routeId);
    const asTopic = resolveTimelineAsTopic(opts);
    const currentState = get(store);
    const canReuseTimeline =
      currentState.timelineThreadId === threadId &&
      currentState.timeline.length > 0;
    patchState({ timelineLoading: true, timelineError: "" });
    try {
      const nextTimeline = asTopic
        ? ((await coreClient.listTopicTimeline(threadId)).events ?? [])
        : ((await coreClient.listThreadTimeline(threadId)).events ?? []);
      if (requestSeq !== timelineRequestSeq) {
        return;
      }
      patchState({
        timelineThreadId: threadId,
        timeline: nextTimeline,
      });
    } catch (e) {
      if (requestSeq !== timelineRequestSeq) {
        return;
      }
      patchState({
        timelineError: `Failed to load timeline: ${e instanceof Error ? e.message : String(e)}`,
        timelineThreadId: canReuseTimeline ? threadId : "",
        timeline: canReuseTimeline ? currentState.timeline : [],
      });
    } finally {
      if (requestSeq === timelineRequestSeq) {
        patchState({ timelineLoading: false });
      }
    }
  }

  async function loadWorkOrders(backingThreadId) {
    const threadId = timelineScopeIdForRoute(backingThreadId);
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

  async function refreshThreadDetail(routeId, flags = {}) {
    const {
      workspace: refreshWorkspace = false,
      snapshot: refreshSnapshot = false,
      documents: refreshDocuments = false,
      timeline: refreshTimeline = false,
      workOrders: refreshWorkOrders = false,
    } = flags;

    const promises = [];
    if (refreshWorkspace || refreshSnapshot || refreshDocuments) {
      promises.push(loadWorkspace(routeId));
    }
    if (refreshTimeline) promises.push(loadTimeline(routeId));
    if (refreshWorkOrders) {
      const sid = get(store).snapshot?.id ?? routeId;
      promises.push(loadWorkOrders(sid));
    }
    await Promise.all(promises);
  }

  async function queueRefreshThreadDetail(routeId, flags = {}) {
    const id = timelineScopeIdForRoute(routeId);
    if (!id) return;

    if (queuedRefreshThreadId && queuedRefreshThreadId !== id) {
      queuedRefreshFlags = null;
      queuedRefreshPromise = null;
    }

    queuedRefreshThreadId = id;
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

  async function fullRefresh(routeId, opts = {}) {
    if (typeof opts.asTopic === "boolean") {
      detailAsTopic = opts.asTopic;
      patchState({ detailAsTopic });
    }
    const id = timelineScopeIdForRoute(routeId);
    await loadWorkspace(id);
    const backingThreadId = get(store).snapshot?.id ?? id;
    await loadWorkOrders(backingThreadId);
  }

  function setSnapshot(value) {
    patchState({ snapshot: value });
  }

  function setDocuments(value) {
    patchState({ documents: value });
  }

  function setTimeline(value, threadId = "") {
    patchState({
      timeline: value,
      timelineThreadId: threadId || "",
    });
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
    detailAsTopic = false;
    queuedRefreshFlags = null;
    queuedRefreshThreadId = "";
    queuedRefreshPromise = null;
    timelineRequestSeq = 0;
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
    setTimeline,
    setWorkOrders,
    getStaleness,
    reset,
  };
}

export const threadDetailStore = createThreadDetailStore();
