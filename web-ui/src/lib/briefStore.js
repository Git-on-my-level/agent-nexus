import { get } from "svelte/store";
import { coreClient } from "$lib/coreClient";
import { normalizeBrief } from "$lib/brief.js";
import { recoverInboxResponses } from "$lib/inboxResponseQueue.js";
import {
  liveInboxChanges,
  liveWorkspaceEvents,
} from "$lib/liveWorkspaceEvents.js";
import { readerScopeKey } from "$lib/readerScope.js";
import { staleWhileRevalidate } from "$lib/staleWhileRevalidate.js";

/**
 * The reader's `brief.v1`, shared by Decide, Watch and the initiative page.
 *
 * One read (`GET /brief`) per refresh, whatever the workspace size: core
 * bounds and materializes it. The snapshot is cached per reader scope, so
 * moving between the three screens paints at once and refreshes behind.
 * A failed refresh keeps the last snapshot and sets `error`; screens show a
 * banner with Retry, never the raw body (I5).
 */

let instance = null;
let instanceScope = "";

function create(scope) {
  return staleWhileRevalidate({
    scope,
    key: `${scope}::brief.v1`,
    currentScope: readerScopeKey,
    // Late results are dropped by the store's generation check.
    read: async () => normalizeBrief(await coreClient.getBrief()),
    maxAgeMs: 15_000,
  });
}

/** The store for the current reader. Recreated when the reader changes. */
export function briefStore() {
  const scope = readerScopeKey();
  if (!instance || instanceScope !== scope) {
    // A reader switch drops the old reader's live subscription with it.
    // This runs from template/derived reads (the nav badge), where writing
    // to a store is forbidden, so teardown is deferred past the read.
    const stopLive = unmount;
    const old = instance;
    unmount = null;
    mounts = 0;
    queueMicrotask(() => {
      stopLive?.();
      old?.destroy();
    });
    instance = create(scope);
    instanceScope = scope;
  }
  return instance;
}

let mounts = 0;
let unmount = null;

/**
 * Mount the brief on a screen: refresh now, on reconnect and visibility, and
 * when an ask or work event lands. Mounts are counted, so the nav badge and
 * the page share one live subscription. Returns the cleanup.
 */
export function mountBrief() {
  const store = briefStore();
  mounts += 1;
  if (mounts === 1) {
    // Answers persisted before a reload or crash resume sending from here.
    void recoverInboxResponses().catch(() => {});
    unmount = store.mount({
      connect: (refresh) => {
        const stopInbox = liveInboxChanges({
          client: coreClient,
          onChange: refresh,
        });
        const stopEvents = liveWorkspaceEvents({
          client: coreClient,
          onChange: refresh,
          debounceMs: 800,
        });
        return () => {
          stopInbox();
          stopEvents();
        };
      },
    });
  } else {
    void store.refresh({ force: false });
  }
  let released = false;
  return () => {
    if (released) return;
    released = true;
    mounts -= 1;
    if (mounts === 0) {
      unmount?.();
      unmount = null;
    }
  };
}

/** Force a refresh, for a confirmed mutation. */
export function refreshBrief() {
  return briefStore().refresh({ force: true, replace: true });
}

/** Current snapshot without subscribing. */
export function currentBrief() {
  return get(briefStore()).data;
}
