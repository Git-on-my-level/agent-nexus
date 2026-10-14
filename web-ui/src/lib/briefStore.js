import { get } from "svelte/store";
import { coreClient } from "$lib/coreClient";
import { normalizeBrief } from "$lib/brief.js";
import { recoverInboxResponses } from "$lib/inboxResponseQueue.js";
import { liveInboxChanges, liveWorkspaceEvents } from "$lib/liveWorkspaceEvents.js";
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
    read: async (signal) => normalizeBrief(await coreClient.getBrief({ signal })),
    maxAgeMs: 15_000,
  });
}

/** The store for the current reader. Recreated when the reader changes. */
export function briefStore() {
  const scope = readerScopeKey();
  if (!instance || instanceScope !== scope) {
    instance?.destroy();
    instance = create(scope);
    instanceScope = scope;
  }
  return instance;
}

/**
 * Mount the brief on a screen: refresh now, on reconnect and visibility, and
 * when an ask or work event lands. Returns the cleanup.
 */
export function mountBrief() {
  const store = briefStore();
  // Answers persisted before a reload or crash resume sending from here.
  void recoverInboxResponses().catch(() => {});
  return store.mount({
    connect: (refresh) => {
      const stopInbox = liveInboxChanges({ client: coreClient, onChange: refresh });
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
}

/** Force a refresh, for a confirmed mutation. */
export function refreshBrief() {
  return briefStore().refresh({ force: true, replace: true });
}

/** Current snapshot without subscribing. */
export function currentBrief() {
  return get(briefStore()).data;
}
