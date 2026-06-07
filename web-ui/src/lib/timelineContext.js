import { writable } from "svelte/store";
import { setContext, getContext } from "svelte";

import { coerceTimelineResourceList } from "./refLinkModel.js";

/** Use Symbol.for so HMR / duplicate module instances still share context. */
const TIMELINE_KEY = Symbol.for("anx.timeline.context");

function timelineEventsFromResult(res) {
  if (res && typeof res === "object" && Array.isArray(res.events)) {
    return res.events;
  }
  return [];
}

function expansionFromTimelineResult(res) {
  if (!res || typeof res !== "object") {
    return {
      timelineArtifacts: [],
      timelineCards: [],
      timelineDocuments: [],
      timelineDocumentRevisions: [],
      timelineThreads: [],
      timelineNotificationReceipts: {},
    };
  }
  return {
    timelineArtifacts: coerceTimelineResourceList(res.artifacts),
    timelineCards: coerceTimelineResourceList(res.cards),
    timelineDocuments: coerceTimelineResourceList(res.documents),
    timelineDocumentRevisions: coerceTimelineResourceList(
      res.document_revisions ?? res.documentRevisions,
    ),
    timelineThreads: coerceTimelineResourceList(res.threads),
    timelineNotificationReceipts:
      res.notification_receipts &&
      typeof res.notification_receipts === "object" &&
      !Array.isArray(res.notification_receipts)
        ? res.notification_receipts
        : {},
  };
}

export function createTimelineContext(coreClient) {
  const store = writable({
    timeline: [],
    timelineLoading: false,
    timelineError: "",
    timelineArtifacts: [],
    timelineCards: [],
    timelineDocuments: [],
    timelineDocumentRevisions: [],
    timelineThreads: [],
    timelineNotificationReceipts: {},
  });

  let loadSeq = 0;
  let lastScopeId = "";
  /** @type {Record<string, unknown>} */
  let lastLoadOpts = {};

  async function loadTimeline(scopeId, opts = {}) {
    const trimmed = String(scopeId ?? "").trim();
    const previousScope = lastScopeId;
    if (trimmed) {
      lastScopeId = trimmed;
    }
    lastLoadOpts = opts && typeof opts === "object" ? { ...opts } : {};

    const seq = ++loadSeq;
    const scopeChanged = Boolean(trimmed && trimmed !== previousScope);
    store.update((s) => ({
      ...s,
      timelineLoading: true,
      timelineError: "",
      ...(scopeChanged
        ? {
            timeline: [],
            timelineArtifacts: [],
            timelineCards: [],
            timelineDocuments: [],
            timelineDocumentRevisions: [],
            timelineThreads: [],
            timelineNotificationReceipts: {},
          }
        : {}),
    }));
    try {
      let res;
      if (lastLoadOpts.asTopic) {
        res = await coreClient.listTopicTimeline(scopeId);
      } else if (lastLoadOpts.asCard) {
        res = await coreClient.listCardTimeline(scopeId);
      } else {
        res = await coreClient.listThreadTimeline(scopeId);
      }
      if (seq !== loadSeq) return;
      const expansion = expansionFromTimelineResult(res);
      store.update((s) => ({
        ...s,
        timeline: timelineEventsFromResult(res),
        timelineLoading: false,
        timelineError: "",
        ...expansion,
      }));
    } catch (err) {
      if (seq !== loadSeq) return;
      const message =
        err && typeof err === "object" && "message" in err
          ? String(err.message)
          : String(err);
      store.update((s) => ({
        ...s,
        timelineLoading: false,
        timelineError: message,
      }));
    }
  }

  function refreshTimeline() {
    if (!lastScopeId) {
      return Promise.resolve();
    }
    return loadTimeline(lastScopeId, lastLoadOpts);
  }

  /**
   * Subscribe to live thread events over SSE and refresh the timeline whenever
   * a new event arrives. Mirrors the topic detail page's stream-with-reconnect
   * so every isolated Discussion surface (board, card, doc) can opt into the
   * same live behavior the topic Messages tab already has.
   *
   * @param {string} scopeId thread id to stream
   * @param {{ lastEventId?: string, reconnectDelayMs?: number }} [opts]
   * @returns {() => void} stop function
   */
  function startLiveUpdates(scopeId, opts = {}) {
    const threadId = String(scopeId ?? "").trim();
    if (!threadId || typeof coreClient.streamThreadEvents !== "function") {
      return () => {};
    }

    let stopped = false;
    let controller = /** @type {AbortController | null} */ (null);
    let reconnectTimer = /** @type {ReturnType<typeof setTimeout> | null} */ (
      null
    );
    let lastEventId = String(opts.lastEventId ?? "").trim();
    const reconnectDelayMs = Number(opts.reconnectDelayMs) || 1_500;

    const connect = async () => {
      if (stopped) return;
      controller = new AbortController();
      try {
        await coreClient.streamThreadEvents({
          threadId,
          lastEventId,
          signal: controller.signal,
          onEvent: async (message) => {
            if (message?.id) {
              lastEventId = message.id;
            }
            if (message?.event !== "event") {
              return;
            }
            await refreshTimeline();
          },
        });
      } catch (err) {
        if (stopped || err?.name === "AbortError") return;
        if (err?.status === 401 || err?.status === 403) return;
      }
      if (!stopped) {
        reconnectTimer = setTimeout(connect, reconnectDelayMs);
      }
    };

    void connect();

    return () => {
      stopped = true;
      controller?.abort();
      if (reconnectTimer) clearTimeout(reconnectTimer);
    };
  }

  return { store, loadTimeline, refreshTimeline, startLiveUpdates };
}

export function setTimelineContext(ctx) {
  setContext(TIMELINE_KEY, ctx);
}

export function getTimelineContext() {
  return getContext(TIMELINE_KEY);
}
