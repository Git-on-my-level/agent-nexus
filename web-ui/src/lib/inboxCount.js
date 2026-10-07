import { humanActorIdSet } from "$lib/humanActors.js";
import { get, writable } from "svelte/store";
import {
  selectedActorId,
  actorRegistry,
  principalRegistry,
} from "$lib/actorSession";
import { buildInboxRows, filterMailbox } from "$lib/inboxMailbox.js";
import { coreClient } from "$lib/coreClient";
import { liveWorkspaceEvents } from "$lib/liveWorkspaceEvents.js";
import {
  applyResponseOverlay,
  inboxResponseOverlay,
} from "$lib/inboxResponseQueue.js";
import { loadInboxSources } from "$lib/inboxSources.js";

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

const REFRESH_DELAY_MS = 1_500;

let pageClaims = 0;
/** @type {null | { workspace: string, users: number, stop: () => void, refresh: () => void }} */
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
  const results = await loadInboxSources({ withHistory: false });
  // A count built on a failed source would claim "clear" when it is not;
  // keep the last number rather than show a wrong one.
  if (results.slice(0, 4).some((result) => result.status === "rejected")) {
    return null;
  }
  const value = (index, key) => results[index].value?.[key] || [];
  return {
    decisions: value(0, "items"),
    actions: value(1, "items"),
    work: value(2, "work"),
    inboxItems: value(3, "items"),
    truncated: results.some(
      (result) =>
        result.status === "fulfilled" &&
        (result.value?.has_more === true || Boolean(result.value?.next_cursor)),
    ),
  };
}

function countFrom(sources, overlay) {
  const rows = buildInboxRows({
    decisions: sources.decisions,
    actions: sources.actions,
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
  const key = String(workspace ?? "").trim();
  if (!key) return () => {};
  if (controller && controller.workspace !== key) {
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
    let sources = null;
    const publish = () => {
      if (stopped || pageClaims || !sources) return;
      publishInboxCount(
        key,
        countFrom(sources, get(inboxResponseOverlay)),
        sources.truncated,
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
    // A response answered from the standalone page lowers the count at once.
    const unsubscribeOverlay = inboxResponseOverlay.subscribe(publish);
    controller = {
      workspace: key,
      users: 1,
      refresh: schedule,
      stop: () => {
        stopped = true;
        clearTimeout(timer);
        unsubscribeLive();
        unsubscribeOverlay();
      },
    };
    const current = get(inboxNeedsYouCount);
    if (current.workspace !== key) publishInboxCount(key, null);
    // Let the route mount and claim its count before starting five background
    // reads. On other pages these badge reads should follow the primary data,
    // rather than competing for core's SQLite connection on first paint.
    schedule();
  }
  return () => {
    if (!controller || controller.workspace !== key) return;
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
