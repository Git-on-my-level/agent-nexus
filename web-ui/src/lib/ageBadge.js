/**
 * Ages, in the same words as every other timestamp.
 *
 * The badge, the provenance line and the row all call `formatTime`, so
 * "15 min ago" cannot become "8m" on the next surface. The verb and the
 * exact local instant live in the tooltip.
 */

import { formatTime, instantMs } from "./time/format.js";

/**
 * Friendly age: "just now", "8 min ago", "3 h ago", "yesterday", "Oct 5".
 *
 * Returns `""` for a missing or unparseable instant, so a caller renders
 * nothing rather than a badge with no content. A future instant reads
 * "in 10 min" or "tomorrow".
 *
 * @param {string|number|Date|null|undefined} value
 * @param {number} [now]
 */
export function formatAge(value, now = Date.now()) {
  if (instantMs(value) == null) return "";
  return formatTime(value, { now, style: "relative" });
}

/**
 * The sentence behind the badge: the verb, and the exact local instant
 * with its timezone abbreviation.
 *
 * @param {string|number|Date|null|undefined} value
 * @param {string} [verb] what happened then ("moved", "updated", "checked")
 * @param {number} [now]
 */
export function ageTitle(value, verb = "", now = Date.now()) {
  if (instantMs(value) == null) return "";
  const absolute = formatTime(value, { now, style: "exact" });
  if (!absolute) return "";
  const action = String(verb ?? "").trim();
  if (!action) return absolute;
  return `${action[0].toUpperCase()}${action.slice(1)} ${absolute}`;
}
