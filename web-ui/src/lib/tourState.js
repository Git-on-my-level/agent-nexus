const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

export function tourStorageKey(workspaceSlug) {
  return `anx.tour.inbox.v1.${workspaceSlug}`;
}

export function firstSeenKey(workspaceSlug) {
  return `anx.tour.inbox.v1.${workspaceSlug}.firstSeen`;
}

export function isTourDismissed(workspaceSlug) {
  if (typeof localStorage === "undefined") return false;
  return localStorage.getItem(tourStorageKey(workspaceSlug)) === "dismissed";
}

export function dismissTour(workspaceSlug) {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(tourStorageKey(workspaceSlug), "dismissed");
}

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

export function shouldShowTour({ workspaceSlug, totalItems }) {
  if (!workspaceSlug) return false;
  if (totalItems > 0) return false;
  if (isTourDismissed(workspaceSlug)) return false;
  return isFirstSeenWithinSevenDays(workspaceSlug);
}
