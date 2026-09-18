import { coreClient } from "./coreClient";
import { coerceTimelineResourceList } from "./refLinkModel.js";
import { get, writable } from "svelte/store";

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
    timelineArtifacts: [],
    timelineCards: [],
    timelineDocuments: [],
    timelineDocumentRevisions: [],
    timelineThreads: [],
    timelineNotificationReceipts: {},
    timelineLoading: false,
    timelineError: "",
    /** When true, workspace/timeline loads use topic-scoped APIs for the route id. */
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

  function receiptDeliveryRank(receipt) {
    const delivery = String(receipt?.delivery_status ?? "").trim();
    if (delivery === "completed" || delivery === "failed") return 2;
    if (delivery === "claimed") return 1;
    if (delivery === "requested") return 0;
    return -1;
  }

  function receiptNotificationRank(receipt) {
    const notification = String(
      receipt?.notification_status ?? receipt?.status ?? "",
    ).trim();
    if (notification === "dismissed") return 2;
    if (notification === "read") return 1;
    if (notification === "unread") return 0;
    return -1;
  }

  function receiptLatestTimestamp(receipt) {
    return [
      receipt?.created_at,
      receipt?.claimed_at,
      receipt?.completed_at,
      receipt?.failed_at,
      receipt?.read_at,
      receipt?.dismissed_at,
    ]
      .map((value) => String(value ?? "").trim())
      .filter(Boolean)
      .sort()
      .at(-1);
  }

  function fresherReceipt(left, right) {
    const leftDeliveryRank = receiptDeliveryRank(left);
    const rightDeliveryRank = receiptDeliveryRank(right);
    if (leftDeliveryRank !== rightDeliveryRank) {
      return leftDeliveryRank > rightDeliveryRank ? left : right;
    }
    const leftNotificationRank = receiptNotificationRank(left);
    const rightNotificationRank = receiptNotificationRank(right);
    if (leftNotificationRank !== rightNotificationRank) {
      return leftNotificationRank > rightNotificationRank ? left : right;
    }
    return String(receiptLatestTimestamp(left) ?? "") >=
      String(receiptLatestTimestamp(right) ?? "")
      ? left
      : right;
  }

  function mergeNotificationReceiptMaps(current, incoming) {
    const currentMap =
      current && typeof current === "object" && !Array.isArray(current)
        ? current
        : {};
    const incomingMap =
      incoming && typeof incoming === "object" && !Array.isArray(incoming)
        ? incoming
        : {};
    const eventIds = new Set([
      ...Object.keys(currentMap),
      ...Object.keys(incomingMap),
    ]);
    const nextMap = {};
    for (const eventId of eventIds) {
      const byWakeup = new Map();
      for (const receipt of [
        ...(Array.isArray(incomingMap[eventId]) ? incomingMap[eventId] : []),
        ...(Array.isArray(currentMap[eventId]) ? currentMap[eventId] : []),
      ]) {
        const wakeupId = String(receipt?.wakeup_id ?? "").trim();
        if (!wakeupId) continue;
        const existing = byWakeup.get(wakeupId);
        byWakeup.set(
          wakeupId,
          existing ? fresherReceipt(existing, receipt) : receipt,
        );
      }
      nextMap[eventId] = Array.from(byWakeup.values());
    }
    return nextMap;
  }

  async function loadWorkspace(routeId) {
    const threadId = timelineScopeIdForRoute(routeId);
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

      const latestState = get(store);
      const canReuseTimeline =
        latestState.timelineThreadId === threadId &&
        latestState.timeline.length > 0;

      let timelinePatch;
      timelinePatch = canReuseTimeline
        ? {}
        : {
            timeline: Array.isArray(context.recent_events)
              ? context.recent_events
              : [],
            timelineArtifacts: Array.isArray(context.key_artifacts)
              ? context.key_artifacts
                  .map((entry) => entry?.artifact ?? entry)
                  .filter(Boolean)
              : [],
            timelineCards: Array.isArray(context.open_cards)
              ? context.open_cards
                  .map((entry) => entry?.card ?? entry)
                  .filter(Boolean)
              : [],
            timelineDocuments: Array.isArray(context.documents)
              ? context.documents
              : [],
            timelineNotificationReceipts: {},
            timelineThreadId: Array.isArray(context.recent_events)
              ? threadId
              : "",
          };

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
          timelineArtifacts: [],
          timelineCards: [],
          timelineDocuments: [],
          timelineDocumentRevisions: [],
          timelineThreads: [],
          timelineNotificationReceipts: {},
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

  async function loadTimeline(routeId) {
    const requestSeq = ++timelineRequestSeq;
    const threadId = timelineScopeIdForRoute(routeId);
    const currentState = get(store);
    const canReuseTimeline =
      currentState.timelineThreadId === threadId &&
      currentState.timeline.length > 0;
    patchState({ timelineLoading: true, timelineError: "" });
    try {
      const result = await coreClient.listThreadTimeline(threadId);
      const nextTimeline = result?.events ?? [];
      if (requestSeq !== timelineRequestSeq) {
        return;
      }
      const incomingNotificationReceipts =
        result?.notification_receipts &&
        typeof result.notification_receipts === "object" &&
        !Array.isArray(result.notification_receipts)
          ? result.notification_receipts
          : {};
      const latestState = get(store);
      patchState({
        timelineThreadId: threadId,
        timeline: nextTimeline,
        timelineArtifacts: coerceTimelineResourceList(result?.artifacts),
        timelineCards: coerceTimelineResourceList(result?.cards),
        timelineDocuments: coerceTimelineResourceList(result?.documents),
        timelineDocumentRevisions: coerceTimelineResourceList(
          result?.document_revisions ?? result?.documentRevisions,
        ),
        timelineThreads: coerceTimelineResourceList(result?.threads),
        timelineNotificationReceipts: mergeNotificationReceiptMaps(
          latestState.timelineThreadId === threadId
            ? latestState.timelineNotificationReceipts
            : {},
          incomingNotificationReceipts,
        ),
      });
    } catch (e) {
      if (requestSeq !== timelineRequestSeq) {
        return;
      }
      patchState({
        timelineError: `Failed to load timeline: ${e instanceof Error ? e.message : String(e)}`,
        timelineThreadId: canReuseTimeline ? threadId : "",
        timeline: canReuseTimeline ? currentState.timeline : [],
        timelineArtifacts: canReuseTimeline
          ? currentState.timelineArtifacts
          : [],
        timelineCards: canReuseTimeline ? currentState.timelineCards : [],
        timelineDocuments: canReuseTimeline
          ? currentState.timelineDocuments
          : [],
        timelineDocumentRevisions: canReuseTimeline
          ? currentState.timelineDocumentRevisions
          : [],
        timelineThreads: canReuseTimeline ? currentState.timelineThreads : [],
        timelineNotificationReceipts: canReuseTimeline
          ? currentState.timelineNotificationReceipts
          : {},
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

  async function fullRefresh(routeId) {
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
      timelineArtifacts: [],
      timelineCards: [],
      timelineDocuments: [],
      timelineDocumentRevisions: [],
      timelineThreads: [],
      timelineNotificationReceipts: {},
      timelineThreadId: threadId || "",
    });
  }

  function patchNotificationReceipt(receipt) {
    if (!receipt || typeof receipt !== "object") return;
    const eventId = String(receipt.trigger_event_id ?? "").trim();
    const wakeupId = String(receipt.wakeup_id ?? "").trim();
    if (!eventId || !wakeupId) return;

    update((state) => {
      const currentMap =
        state.timelineNotificationReceipts &&
        typeof state.timelineNotificationReceipts === "object" &&
        !Array.isArray(state.timelineNotificationReceipts)
          ? state.timelineNotificationReceipts
          : {};
      const currentReceipts = Array.isArray(currentMap[eventId])
        ? currentMap[eventId]
        : [];
      const nextReceipts = [...currentReceipts];
      const index = nextReceipts.findIndex(
        (item) => String(item?.wakeup_id ?? "").trim() === wakeupId,
      );
      if (index >= 0) {
        nextReceipts[index] = fresherReceipt(nextReceipts[index], receipt);
      } else {
        nextReceipts.push(receipt);
      }
      return {
        ...state,
        timelineNotificationReceipts: {
          ...currentMap,
          [eventId]: nextReceipts,
        },
      };
    });
  }

  function reset() {
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
    patchNotificationReceipt,
    reset,
  };
}

export const topicDetailStore = createTopicDetailStore();
