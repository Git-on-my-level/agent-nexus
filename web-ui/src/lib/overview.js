export { humanActorIdSet, isHumanNextActor } from "$lib/humanActors.js";
import { parseVisualReport } from "$lib/visualReports.js";

/** `GET /work` page size (contract maximum) and how many rows Overview will read. */
export const WORK_PAGE_LIMIT = 200;
export const WORK_ROW_CAP = 2000;
/** Full `human=1` rescans follow live events at most this often. */
const HUMAN_LIVE_REFRESH_MS = 10_000;

/**
 * Delay before the next full human-filter rescan. `0` means run now.
 * @param {number} now
 * @param {number} lastAt
 * @param {number} [interval]
 */
export function humanLiveRefreshDelay(
  now,
  lastAt,
  interval = HUMAN_LIVE_REFRESH_MS,
) {
  const elapsed = Number(now) - Number(lastAt || 0);
  if (!Number.isFinite(elapsed) || elapsed >= interval) return 0;
  return interval - elapsed;
}

// Preserve loaded choices while advancing through the API's candidate windows.
export function mergeDashboardReports(current, next) {
  const reports = new Map();
  for (const entry of [...(current?.reports || []), ...(next?.reports || [])]) {
    const parsed = parseVisualReport(JSON.stringify(entry.report));
    if (parsed.report)
      reports.set(entry.id, {
        ...entry,
        ...(reports.get(entry.id)?.requested_as
          ? { requested_as: reports.get(entry.id).requested_as }
          : {}),
        report: parsed.report,
      });
  }
  return {
    ...current,
    ...next,
    pinned_ref: current?.pinned_ref ?? next?.pinned_ref ?? null,
    reports: [...reports.values()],
  };
}
