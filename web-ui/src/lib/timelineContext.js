import { writable } from "svelte/store";
import { setContext, getContext } from "svelte";

/** Use Symbol.for so HMR / duplicate module instances still share context. */
const TIMELINE_KEY = Symbol.for("anx.timeline.context");

function timelineEventsFromResult(res) {
  if (res && typeof res === "object" && Array.isArray(res.events)) {
    return res.events;
  }
  return [];
}

export function createTimelineContext(coreClient) {
  const store = writable({
    timeline: [],
    timelineLoading: false,
    timelineError: "",
  });

  let loadSeq = 0;
  let lastScopeId = "";

  async function loadTimeline(scopeId, opts = {}) {
    const trimmed = String(scopeId ?? "").trim();
    if (trimmed) {
      lastScopeId = trimmed;
    }
    const seq = ++loadSeq;
    store.update((s) => ({
      ...s,
      timelineLoading: true,
      timelineError: "",
    }));
    try {
      let res;
      if (opts?.asTopic) {
        res = await coreClient.listTopicTimeline(scopeId);
      } else if (opts?.asCard) {
        res = await coreClient.listCardTimeline(scopeId);
      } else {
        res = await coreClient.listThreadTimeline(scopeId);
      }
      if (seq !== loadSeq) return;
      store.update((s) => ({
        ...s,
        timeline: timelineEventsFromResult(res),
        timelineLoading: false,
        timelineError: "",
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
    return loadTimeline(lastScopeId);
  }

  return { store, loadTimeline, refreshTimeline };
}

export function setTimelineContext(ctx) {
  setContext(TIMELINE_KEY, ctx);
}

export function getTimelineContext() {
  return getContext(TIMELINE_KEY);
}
