import { get, writable } from "svelte/store";

import { coreClient } from "$lib/coreClient";
import { isAdministrationRefusal } from "$lib/coreAuthErrors.js";

/**
 * How many access requests are waiting for a decision: the number behind the
 * Access badge in the shell.
 *
 * Two things land here: agents asking for a grant (`GET /auth/access-requests`)
 * and machines asking to enroll (`anx host enroll`). The badge and the Access
 * page count the same way, so the number on the menu and the number on the
 * page cannot disagree.
 *
 * It counts what the reader can still decide. Core's `GET /auth/access/summary`
 * is the cheaper read, but its `pending_count` also includes an approved
 * enrollment waiting on its machine — a row whose approve control is disabled
 * and whose only end is the machine finishing or the ceremony expiring. A
 * badge for that nags about something nobody can clear, so this counts from
 * the two lists instead and pays one extra request per poll.
 *
 * Deciding access is human-only (`/auth/access-requests`), so the shell only
 * runs this for a person. A refusal still hides the badge rather than showing
 * an error, and an expired session is not a refusal (see
 * `isAdministrationRefusal`), so a 401 keeps polling.
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

function list(value) {
  return Array.isArray(value) ? value : [];
}

/**
 * An enrollment still waiting on the reader: pending, and not expired.
 *
 * An approved ceremony is waiting on its machine — `HostEnrollmentRequest`
 * disables its approve control and labels it "Approved, awaiting completion" —
 * so it is not a decision. An enrollment whose expiry cannot be read is not
 * counted either: the UI cannot show it as live without knowing that it is.
 */
export function pendingEnrollment(entry, now = Date.now()) {
  if (String(entry?.status ?? "pending") !== "pending") return false;
  const expiresAt = Date.parse(entry?.expires_at ?? "");
  return Number.isFinite(expiresAt) ? expiresAt > now : false;
}

/** A request an agent made that nobody has decided yet. */
function pendingRequest(entry) {
  return String(entry?.status ?? "pending") === "pending";
}

/**
 * The badge number for a set of pending sources.
 *
 * @param {{ enrollments?: object[], accessRequests?: object[] }} sources
 * @param {number} [now]
 */
export function countPendingAccessItems(sources = {}, now = Date.now()) {
  return (
    list(sources.accessRequests).filter(pendingRequest).length +
    list(sources.enrollments).filter((entry) => pendingEnrollment(entry, now))
      .length
  );
}

async function fetchPendingSources() {
  const [requests, enrollments] = await Promise.all([
    coreClient.listAccessRequests(),
    coreClient.listPendingHostEnrollments(),
  ]);
  return {
    accessRequests: requests?.requests ?? [],
    enrollments: enrollments?.enrollments ?? [],
  };
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
 * @param {{ enrollments?: object[], accessRequests?: object[] }} sources
 */
export function publishPendingAccessSources(workspace, sources) {
  publishPendingAccessTotal(workspace, countPendingAccessItems(sources));
}

function publishPendingAccessTotal(workspace, count) {
  pendingAccessCount.set({
    workspace: String(workspace ?? ""),
    count: Math.max(0, Number(count) || 0),
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
        // Clear the refusal latch: the reader may have signed in as someone
        // else since, and a latched controller would otherwise leave whatever
        // number the Access page last published frozen for the session.
        forbidden = false;
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

/**
 * Forget the number entirely.
 *
 * A count read by one principal must not survive into another's session: it
 * was computed from an inventory the next reader may not be allowed to see,
 * and nothing would ever refresh it once polling stops.
 */
export function clearPendingAccessCount() {
  pendingAccessCount.set({ workspace: "", count: null, forbidden: false });
}

/** Test hook. */
export function resetPendingAccessCount() {
  controller?.stop();
  controller = null;
  pageClaims = 0;
  pendingAccessCount.set({ workspace: "", count: null, forbidden: false });
}
