/**
 * Ages, as a badge rather than a sentence.
 *
 * "moved 2h ago" spends four words on one fact and still has to be read
 * left to right to find it. A badge says `2h`, and the verb and the exact
 * instant live in the tooltip and the accessible name, where they cost no
 * width at all.
 *
 * The vocabulary matches `formatTimestamp` so a relative age reads the same
 * whichever form a surface uses.
 */

import { formatAbsoluteDateTime } from "./formatDate.js";

/**
 * Compact age: `now`, `8m`, `8h`, `3d`, `5w`, `2y`.
 *
 * Returns `""` for a missing or unparseable instant, so a caller renders
 * nothing rather than a badge with no content. A future instant reads
 * `in 2d`, because a due date in the future is a real thing to show.
 *
 * @param {string|number|Date|null|undefined} value
 * @param {number} [now]
 */
export function formatAge(value, now = Date.now()) {
  const at = value ? new Date(value).getTime() : NaN;
  if (!Number.isFinite(at)) return "";
  const elapsed = Number(now) - at;
  const ahead = elapsed < 0;
  const minutes = Math.floor(Math.abs(elapsed) / 60_000);
  if (minutes < 1) return "now";
  const compact = (() => {
    if (minutes < 60) return `${minutes}m`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h`;
    const days = Math.floor(hours / 24);
    if (days < 14) return `${days}d`;
    const weeks = Math.floor(days / 7);
    if (weeks < 52) return `${weeks}w`;
    return `${Math.floor(days / 365)}y`;
  })();
  return ahead ? `in ${compact}` : compact;
}

/**
 * The sentence behind the badge: the verb, and the exact instant.
 *
 * `Moved Oct 5, 2026, 11:12` — a reader who wants the timestamp hovers for
 * it, and a screen reader gets it as the badge's accessible name.
 *
 * @param {string|number|Date|null|undefined} value
 * @param {string} [verb] what happened then ("moved", "updated", "checked")
 * @param {number} [now]
 */
export function ageTitle(value, verb = "", now = Date.now()) {
  // `formatAbsoluteDateTime` echoes an unparseable value back; a badge shows
  // nothing at all for one, so its tooltip must say nothing either.
  const at = value ? new Date(value).getTime() : NaN;
  if (!Number.isFinite(at)) return "";
  const absolute = formatAbsoluteDateTime(value);
  if (!absolute) return "";
  const action = String(verb ?? "").trim();
  const relative = formatAge(value, now);
  const suffix = relative && relative !== "now" ? ` (${relative})` : "";
  if (!action) return `${absolute}${suffix}`;
  const sentence = `${action[0].toUpperCase()}${action.slice(1)}`;
  return `${sentence} ${absolute}${suffix}`;
}
