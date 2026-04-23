import { writable } from "svelte/store";

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

/** @param {string} workspaceSlug */
export function tourStorageKey(workspaceSlug) {
  return `anx.tour.inbox.v1.${workspaceSlug}`;
}

/** @param {string} workspaceSlug */
export function firstSeenKey(workspaceSlug) {
  return `anx.tour.inbox.v1.${workspaceSlug}.firstSeen`;
}

/**
 * @deprecated Use workspace spotlight tour (`isWorkspaceTourSeen` / `markWorkspaceTourSeen`) instead.
 * @param {string} workspaceSlug
 */
export function isTourDismissed(workspaceSlug) {
  if (typeof localStorage === "undefined") return false;
  return localStorage.getItem(tourStorageKey(workspaceSlug)) === "dismissed";
}

/**
 * @deprecated Use `markWorkspaceTourSeen` instead.
 * @param {string} workspaceSlug
 */
export function dismissTour(workspaceSlug) {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(tourStorageKey(workspaceSlug), "dismissed");
}

/**
 * @deprecated Kept for backwards compatibility; new onboarding does not use first-seen windowing.
 * @param {string} workspaceSlug
 */
export function isFirstSeenWithinSevenDays(workspaceSlug) {
  if (typeof localStorage === "undefined") return false;
  const key = firstSeenKey(workspaceSlug);
  const stored = localStorage.getItem(key);
  if (stored) {
    const firstSeen = Number(stored);
    if (!Number.isNaN(firstSeen) && Date.now() - firstSeen < SEVEN_DAYS_MS) {
      return true;
    }
    return false;
  }
  localStorage.setItem(key, String(Date.now()));
  return true;
}

/**
 * @deprecated Use workspace spotlight tour eligibility instead.
 * @param {{ workspaceSlug: string, totalItems: number }} args
 */
export function shouldShowTour({ workspaceSlug, totalItems }) {
  if (!workspaceSlug) return false;
  if (totalItems > 0) return false;
  if (isTourDismissed(workspaceSlug)) return false;
  return isFirstSeenWithinSevenDays(workspaceSlug);
}

// --- Cold-start workspace spotlight (left-nav) tour ---

/** @param {string} workspaceSlug */
export function workspaceTourSeenKey(workspaceSlug) {
  return `workspaceTourSeen.${workspaceSlug}`;
}

/**
 * @param {string} workspaceSlug
 * @returns {boolean}
 */
export function isWorkspaceTourSeen(workspaceSlug) {
  if (typeof localStorage === "undefined") return true;
  return localStorage.getItem(workspaceTourSeenKey(workspaceSlug)) === "1";
}

/**
 * @param {string} workspaceSlug
 */
export function markWorkspaceTourSeen(workspaceSlug) {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(workspaceTourSeenKey(workspaceSlug), "1");
}

// --- Workspace activation milestones (used by the cold-start banner) ---

/** @param {string} workspaceSlug */
export function workspaceTourArrivalKey(workspaceSlug) {
  return `workspaceTourArrival.${workspaceSlug}`;
}

/**
 * Records that the user has arrived at the Access page via the cold-start
 * tour at least once. Used to keep the "connect your first agent" banner
 * visible across reloads/navigation until the activation milestone is hit.
 * @param {string} workspaceSlug
 */
export function markWorkspaceTourArrived(workspaceSlug) {
  if (typeof localStorage === "undefined") return;
  if (!workspaceSlug) return;
  localStorage.setItem(workspaceTourArrivalKey(workspaceSlug), "1");
}

/**
 * @param {string} workspaceSlug
 * @returns {boolean}
 */
export function isWorkspaceTourArrived(workspaceSlug) {
  if (typeof localStorage === "undefined") return false;
  if (!workspaceSlug) return false;
  return localStorage.getItem(workspaceTourArrivalKey(workspaceSlug)) === "1";
}

// --- Replay signal (Home "Take the tour" button → WorkspaceTour) ---

/**
 * Monotonic counter; subscribers re-open the workspace tour each time it
 * increments. Plain in-memory store (the replay command is per-tab).
 */
export const replayTourSignal = writable(0);

/** Trigger a workspace-tour replay. */
export function replayWorkspaceTour() {
  replayTourSignal.update((n) => n + 1);
}

/** @param {string} kind */
export function inboxKindSeenKey(kind) {
  return `inboxKindSeen.${kind}`;
}

/**
 * @param {string} kind
 * @returns {boolean}
 */
export function isInboxKindSeen(kind) {
  if (typeof localStorage === "undefined") return true;
  const k = String(kind ?? "").trim() || "unknown";
  return localStorage.getItem(inboxKindSeenKey(k)) === "1";
}

/**
 * @param {string} kind
 */
export function markInboxKindSeen(kind) {
  if (typeof localStorage === "undefined") return;
  const k = String(kind ?? "").trim() || "unknown";
  localStorage.setItem(inboxKindSeenKey(k), "1");
}
