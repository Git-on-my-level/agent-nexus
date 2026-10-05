import { get, writable } from "svelte/store";

import { coreClient } from "$lib/coreClient";
import { isAdministrationRefusal } from "$lib/coreAuthErrors.js";

/**
 * How many access requests are waiting for a decision: the number behind the
 * Access badge in the shell.
 *
 * Today that is pending host enrollments (`anx host enroll` ceremonies still
 * awaiting approval). Structured access requests join the same number when
 * that primitive lands; `countPendingAccessItems` and `fetchPendingSources`
 * are the two places to extend.
 *
 * Reading the pending list needs administration authority. A principal
 * without it is refused, which is not an error worth showing: the badge
 * simply never appears, and the poll stops rather than retrying forever. An
 * expired session is not a refusal (see `isAdministrationRefusal`) and must
 * not hide the badge for the rest of the session, so it keeps polling.
 *
 * While the Access page is open it publishes the list it already polls, so
 * the badge and the page never disagree and the shell stops fetching.
 */
export const pendingAccessCount = writable(
  /** @type {{ workspace: string, count: number | null, forbidden: boolean }} */ ({
    workspace: "",
    count: null,
    forbidden: false,
  }),
);

const REFRESH_MS = 20_000;

/**
 * The one phrasing of this number, shared by the badge and by the account
 * menu trigger whose aria-label has to carry it.
 *
 * @param {number} count
 */
export function pendingAccessLabel(count) {
  const value = Number(count) || 0;
  return value === 1
    ? "1 access request waiting"
    : `${value} access requests waiting`;
}

/** An enrollment the reader can still approve or deny. */
function decidable(entry) {
  return String(entry?.status ?? "pending") === "pending";
}

/**
 * The badge number for a set of pending sources. Approved ceremonies waiting
 * on the machine to finish are not a decision, so they are not counted.
 *
 * @param {{ enrollments?: object[] }} sources
 */
export function countPendingAccessItems(sources = {}) {
  const enrollments = Array.isArray(sources.enrollments)
    ? sources.enrollments
    : [];
  return enrollments.filter(decidable).length;
}

async function fetchPendingSources() {
  const enrollments = await coreClient.listPendingHostEnrollments();
  return { enrollments: enrollments?.enrollments ?? [] };
}

let pageClaims = 0;
/** @type {null | {
 *   workspace: string,
 *   users: number,
 *   stop: () => void,
 *   refresh: () => void,
 *   suspend: () => void,
 * }} */
let controller = null;

/** The Access page owns the number while mounted. Returns a release function. */
export function claimPendingAccessCount() {
  pageClaims += 1;
  // Stand the shell's own poll down rather than letting a scheduled tick
  // fetch what the page is about to publish.
  controller?.suspend();
  let released = false;
  return () => {
    if (released) return;
    released = true;
    pageClaims = Math.max(0, pageClaims - 1);
    // Leaving Access: from here on the badge keeps itself current.
    if (!pageClaims) controller?.refresh();
  };
}

/**
 * @param {string} workspace
 * @param {{ enrollments?: object[] }} sources
 */
export function publishPendingAccessSources(workspace, sources) {
  pendingAccessCount.set({
    workspace: String(workspace ?? ""),
    count: countPendingAccessItems(sources),
    forbidden: false,
  });
}

/** The reader may not read pending access; the badge stays hidden. */
export function publishPendingAccessForbidden(workspace) {
  pendingAccessCount.set({
    workspace: String(workspace ?? ""),
    count: null,
    forbidden: true,
  });
}

/**
 * Keep the number for `workspace` current while at least one badge shows it.
 * Returns a stop function.
 *
 * @param {string} workspace
 * @param {{ refreshMs?: number }} [options] poll interval; tests shorten it
 */
export function startPendingAccessCount(workspace, options = {}) {
  const key = String(workspace ?? "").trim();
  const refreshMs =
    Number(options.refreshMs) > 0 ? Number(options.refreshMs) : REFRESH_MS;
  if (!key) return () => {};
  if (controller && controller.workspace !== key) {
    controller.stop();
    controller = null;
  }
  if (controller) {
    controller.users += 1;
  } else {
    let stopped = false;
    let forbidden = false;
    let timer = null;
    let inflight = false;
    const schedule = () => {
      if (stopped || forbidden) return;
      clearTimeout(timer);
      timer = setTimeout(() => void run(), refreshMs);
    };
    const run = async () => {
      if (stopped || forbidden) return;
      if (pageClaims || (typeof document !== "undefined" && document.hidden)) {
        // The Access page publishes while it is open, and a hidden tab has
        // nobody looking at the badge. Check again on the next tick.
        schedule();
        return;
      }
      if (inflight) return;
      inflight = true;
      try {
        const sources = await fetchPendingSources();
        if (!stopped && !pageClaims) publishPendingAccessSources(key, sources);
      } catch (error) {
        if (isAdministrationRefusal(error)) {
          // Not an error to report: this reader cannot see pending access.
          forbidden = true;
          if (!stopped) publishPendingAccessForbidden(key);
          return;
        }
        // Keep the last number; the Access page reports load failures.
      } finally {
        inflight = false;
        schedule();
      }
    };
    controller = {
      workspace: key,
      users: 1,
      refresh: () => {
        clearTimeout(timer);
        void run();
      },
      suspend: () => {
        clearTimeout(timer);
      },
      stop: () => {
        stopped = true;
        clearTimeout(timer);
      },
    };
    const current = get(pendingAccessCount);
    if (current.workspace !== key) {
      pendingAccessCount.set({ workspace: key, count: null, forbidden: false });
    }
    void run();
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
export function resetPendingAccessCount() {
  controller?.stop();
  controller = null;
  pageClaims = 0;
  pendingAccessCount.set({ workspace: "", count: null, forbidden: false });
}
