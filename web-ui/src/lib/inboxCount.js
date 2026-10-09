import { readerScope, readerScopeKey } from "$lib/readerScope.js";
import {
  readWorkspaceView,
  writeWorkspaceView,
} from "$lib/workspaceViewCache.js";
import { humanActorIdSet } from "$lib/humanActors.js";
import { get, writable } from "svelte/store";
import {
  selectedActorId,
  actorRegistry,
  principalRegistry,
} from "$lib/actorSession";
import { buildInboxRows, filterMailbox } from "$lib/inboxMailbox.js";
import { coreClient } from "$lib/coreClient";
import {
  liveWorkspaceEvents,
  liveInboxChanges,
} from "$lib/liveWorkspaceEvents.js";
import {
  applyResponseOverlay,
  inboxResponseOverlay,
} from "$lib/inboxResponseQueue.js";
import {
  loadInboxSources,
  mergeInboxItems,
  hasCompleteInboxHistory,
} from "$lib/inboxSources.js";

/**
 * The sidebar's Inbox count: how many rows sit in Needs you.
 *
 * While the Inbox page is open it publishes the count it already computed
 * (including responses still inside their undo window), so the badge and the
 * tab never disagree and nothing is fetched twice. Elsewhere this module
 * loads the Needs you sources itself and refreshes on workspace events.
 */
export const inboxNeedsYouCount = writable(
  /** @type {{ workspace: string, count: number | null, truncated: boolean }} */ ({
    workspace: "",
    count: null,
    truncated: false,
  }),
);

let countScope = readerScopeKey();
readerScope.subscribe((scope) => {
  if (scope === countScope) return;
  countScope = scope;
  inboxNeedsYouCount.set({ workspace: "", count: null, truncated: false });
});

const REFRESH_DELAY_MS = 1_500;

let pageClaims = 0;
/** @type {null | { workspace: string, scope: string, users: number, stop: () => void, refresh: () => void }} */
let controller = null;

/** The Inbox page owns the count while mounted. Returns a release function. */
export function claimInboxCount() {
  pageClaims += 1;
  let released = false;
  return () => {
    if (released) return;
    released = true;
    pageClaims = Math.max(0, pageClaims - 1);
    // Leaving the Inbox: from here on the badge keeps itself current.
    if (!pageClaims) controller?.refresh();
  };
}

export function publishInboxCount(workspace, count, truncated = false) {
  inboxNeedsYouCount.set({
    workspace: String(workspace ?? ""),
    count: Number.isFinite(count) ? count : null,
    truncated: Boolean(truncated),
  });
}

async function fetchSources() {
  const scope = readerScopeKey();
  const results = await loadInboxSources({ withHistory: false });
  if (scope !== readerScopeKey()) return null;
  /*
   * A count built on a failed source would claim "clear" when it is not; keep
   * the last number rather than show a wrong one.
   *
   * Completed items (index 4) are in that set, not optional colour: they are
   * what tells `buildInboxRows` an ask has been answered. Losing them inflates
   * the count rather than shrinking it, which is the worse direction to be
   * wrong in.
   */
  if (
    results.slice(0, 5).some((result) => result.status !== "fulfilled") ||
    !hasCompleteInboxHistory(results)
  ) {
    return null;
  }
  if (
    results.slice(0, 5).every((result) => result.complete && !result.reason)
  ) {
    const previous = readWorkspaceView(`${scope}:inbox`);
    writeWorkspaceView(`${scope}:inbox`, [
      ...results.slice(0, 5),
      previous?.[5] || results[5],
    ]);
  }
  return sourcesFromResults(results);
}

function sourcesFromResults(results) {
  const value = (index, key) => results[index].value?.[key] || [];
  return {
    decisions: value(0, "items"),
    actions: value(1, "items"),
    receiptsUnavailable: !results[1].complete,
    work: value(2, "work"),
    /*
     * Open *and* completed, exactly as the Inbox page merges them.
     *
     * A blocked card with an explicit ask is represented by that ask, and it
     * stays out of Needs you once the ask is answered: the answer released the
     * human, and the agent still owns moving the card. `buildInboxRows` knows
     * that only from the completed ask's `responded_at`. Counting from the
     * open items alone left that map empty, so the card came back — which is
     * why answering something in the Inbox and then leaving it brought the
     * badge back for an item the reader had already dealt with.
     */
    inboxItems: mergeInboxItems(value(3, "items"), value(4, "items")),
    truncated: results.some(
      (result) =>
        result.status === "fulfilled" &&
        (result.complete === false ||
          result.value?.has_more === true ||
          Boolean(result.value?.next_cursor)),
    ),
  };
}

function countFrom(sources, overlay) {
  const rows = buildInboxRows({
    decisions: sources.decisions,
    actions: sources.actions,
    receiptsUnavailable: sources.receiptsUnavailable,
    work: sources.work,
    inboxItems: applyResponseOverlay(sources.inboxItems, overlay),
    currentActorId: get(selectedActorId) || "",
    humanIds: humanActorIdSet(get(actorRegistry), get(principalRegistry)),
  });
  return filterMailbox(rows, "needs-you").length;
}

/**
 * Keep the count for `workspace` current while at least one badge shows it.
 * Returns a stop function.
 */
export function startInboxCount(workspace) {
  const scope = readerScopeKey();
  const key = String(workspace ?? "").trim();
  if (!key) return () => {};
  if (
    controller &&
    (controller.workspace !== key || controller.scope !== scope)
  ) {
    controller.stop();
    controller = null;
  }
  if (controller) {
    controller.users += 1;
  } else {
    let stopped = false;
    let timer = null;
    let inflight = false;
    let again = false;
    const cached = readWorkspaceView(`${scope}:inbox`);
    let sources = cached ? sourcesFromResults(cached) : null;
    let streamPartial = false;
    const publish = () => {
      if (stopped || pageClaims || !sources || scope !== readerScopeKey())
        return;
      publishInboxCount(
        key,
        countFrom(sources, get(inboxResponseOverlay)),
        sources.truncated || streamPartial,
      );
    };
    const run = async () => {
      if (stopped || pageClaims) return;
      if (inflight) {
        again = true;
        return;
      }
      inflight = true;
      try {
        const next = await fetchSources();
        if (next) sources = next;
        publish();
      } catch {
        // Keep the last count; the Inbox page reports load failures.
      } finally {
        inflight = false;
        if (again && !stopped) {
          again = false;
          void run();
        }
      }
    };
    const schedule = () => {
      if (stopped || pageClaims) return;
      clearTimeout(timer);
      timer = setTimeout(() => void run(), REFRESH_DELAY_MS);
    };
    const unsubscribeLive = liveWorkspaceEvents({
      client: coreClient,
      onChange: schedule,
    });
    const unsubscribeInbox = liveInboxChanges({
      client: coreClient,
      onChange: (changes) => {
        for (const change of changes) {
          if (change.type === "inbox_page") streamPartial = change.partial;
        }
        publish();
        schedule();
      },
    });
    // A response answered from the standalone page lowers the count at once.
    const unsubscribeOverlay = inboxResponseOverlay.subscribe(publish);
    controller = {
      workspace: key,
      scope,
      users: 1,
      refresh: schedule,
      stop: () => {
        stopped = true;
        clearTimeout(timer);
        unsubscribeLive();
        unsubscribeInbox();
        unsubscribeOverlay();
      },
    };
    const current = get(inboxNeedsYouCount);
    if (current.workspace !== key) publishInboxCount(key, null);
    // Let the route mount and claim its count before starting five background
    // reads. On other pages these badge reads should follow the primary data,
    // rather than competing for core's SQLite connection on first paint.
    publish();
    schedule();
  }
  const owner = controller;
  return () => {
    if (!controller || controller !== owner) return;
    controller.users -= 1;
    if (controller.users <= 0) {
      controller.stop();
      controller = null;
    }
  };
}

/** Test hook. */
export function resetInboxCount() {
  controller?.stop();
  controller = null;
  pageClaims = 0;
  publishInboxCount("", null);
}
