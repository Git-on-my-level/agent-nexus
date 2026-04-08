import { coreClient } from "./coreClient";
import { splitTypedRef } from "./inboxUtils.js";
import {
  boardOwnsTopicId,
  resolveBoardCardThreadIdField,
} from "./topicRouteUtils.js";
import { computeStaleness } from "./topicFilters";
import { get, writable } from "svelte/store";

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
    .filter((b) => boardOwnsTopicId(b, topicIdStr))
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
    const cid = resolveBoardCardThreadIdField(card);
    if (!primaryThreadId || cid !== primaryThreadId) continue;
    const fromBoardRef = splitTypedRef(String(card?.board_ref ?? "").trim());
    const boardId =
      String(card?.board_id ?? "").trim() ||
      (fromBoardRef.prefix === "board" ? fromBoardRef.id : "");
    if (!boardId) continue;
    const board = boardById.get(boardId) ?? { id: boardId, title: boardId };
    boardMemberships.push({ board, card });
  }

  return { ownedBoards, boardMemberships };
}

function initialState() {
  return {
    workspace: null,
    topic: null,
    topicLoading: false,
    topicError: "",
    documents: [],
    documentsLoading: false,
    documentsError: "",
    boardMemberships: [],
    ownedBoards: [],
    timelineThreadId: "",
    timeline: [],
    timelineLoading: false,
    timelineError: "",
    /** When true, workspace/timeline loads use topic-scoped APIs for the route id. */
    detailAsTopic: false,
  };
}

function createTopicDetailStore() {
  const store = writable(initialState());
  const { subscribe, update, set } = store;
  const patchState = (patch) => update((state) => ({ ...state, ...patch }));
  let queuedRefreshFlags = null;
  let queuedRefreshRouteId = "";
  let queuedRefreshPromise = null;
  let timelineRequestSeq = 0;
  /** Controls thread vs topic API for workspace/timeline refresh coalescing. */
  let detailAsTopic = false;

  function mergeRefreshFlags(base, next) {
    const left = base ?? {};
    const right = next ?? {};
    return {
      workspace: Boolean(left.workspace || right.workspace),
      topic: Boolean(left.topic || right.topic),
      documents: Boolean(left.documents || right.documents),
      timeline: Boolean(left.timeline || right.timeline),
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
      currentState.topic !== null ||
      currentState.documents.length > 0 ||
      currentState.timeline.length > 0;

    patchState({
      topicLoading: hasWorkspaceData ? currentState.topicLoading : true,
      topicError: hasWorkspaceData ? currentState.topicError : "",
      documentsLoading: hasWorkspaceData ? currentState.documentsLoading : true,
      documentsError: hasWorkspaceData ? currentState.documentsError : "",
    });
    try {
      let workspace;
      let topic;
      let documents = [];
      let boardMemberships = [];
      let ownedBoards = [];

      if (asTopic) {
        workspace = await coreClient.getTopicWorkspace(threadId, {});
        const primaryThread = primaryThreadFromTopicWorkspace(workspace);
        topic = primaryThread;
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
        topic = workspace?.thread ?? null;
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
        // Topic timelines come only from `listTopicTimeline`, never from workspace payloads.
        // Clear only when switching topics — not when the in-flight timeline is empty (e.g. parallel
        // workspace+timeline refresh after posting would otherwise wipe the store and lose new events).
        const prevTimelineScope = String(
          latestState.timelineThreadId ?? "",
        ).trim();
        const timelineStale =
          prevTimelineScope !== "" && prevTimelineScope !== threadId;
        timelinePatch = timelineStale
          ? { timeline: [], timelineThreadId: "" }
          : {};
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
        topic,
        topicError: "",
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
          topicError: `Failed to load workspace: ${message}`,
          topic: null,
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
        topicLoading: false,
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

  async function refreshTopicDetail(routeId, flags = {}) {
    const {
      workspace: refreshWorkspace = false,
      topic: refreshTopic = false,
      documents: refreshDocuments = false,
      timeline: refreshTimeline = false,
    } = flags;

    const promises = [];
    if (refreshWorkspace || refreshTopic || refreshDocuments) {
      promises.push(loadWorkspace(routeId));
    }
    if (refreshTimeline) promises.push(loadTimeline(routeId));
    await Promise.all(promises);
  }

  async function queueRefreshTopicDetail(routeId, flags = {}) {
    const id = timelineScopeIdForRoute(routeId);
    if (!id) return;

    if (queuedRefreshRouteId && queuedRefreshRouteId !== id) {
      queuedRefreshFlags = null;
      queuedRefreshPromise = null;
    }

    queuedRefreshRouteId = id;
    queuedRefreshFlags = mergeRefreshFlags(queuedRefreshFlags, flags);

    if (queuedRefreshPromise) {
      return queuedRefreshPromise;
    }

    queuedRefreshPromise = (async () => {
      while (queuedRefreshFlags) {
        const nextFlags = queuedRefreshFlags;
        queuedRefreshFlags = null;
        await refreshTopicDetail(queuedRefreshRouteId, nextFlags);
      }
    })().finally(() => {
      queuedRefreshPromise = null;
      queuedRefreshRouteId = "";
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
  }

  function setTopic(value) {
    patchState({ topic: value });
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

  function getStaleness(topic) {
    const value = topic ?? get(store).topic;
    if (!value) return null;
    return computeStaleness(value);
  }

  function reset() {
    detailAsTopic = false;
    queuedRefreshFlags = null;
    queuedRefreshRouteId = "";
    queuedRefreshPromise = null;
    timelineRequestSeq = 0;
    set(initialState());
  }

  return {
    subscribe,
    loadWorkspace,
    loadTimeline,
    refreshTopicDetail,
    queueRefreshTopicDetail,
    fullRefresh,
    setTopic,
    setDocuments,
    setTimeline,
    getStaleness,
    reset,
  };
}

export const topicDetailStore = createTopicDetailStore();
