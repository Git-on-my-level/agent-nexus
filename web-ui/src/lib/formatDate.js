/**
 * Display wrappers around `time/format.js`.
 *
 * New code should render `<Time>` or call `formatTime` directly. These names
 * stay so a sentence built in a module — a provenance line, a core message
 * with an ISO instant in it — does not grow a second formatter.
 *
 * The relative form is "just now", "15 min ago", "3 h ago", "yesterday",
 * then a short local date ("Oct 5", or "Oct 5, 2025" in another year).
 * Future times read "in 10 min" or "tomorrow". Do not prefix the result
 * with "on" or "at": that reads as "archived on 3 h ago". Word the sentence
 * so both forms work, and let `<Time>` carry the exact local instant.
 */

export {
  datetimeLocalToIso,
  formatElapsed,
  formatTime,
  instantIso,
  isoToDatetimeLocal,
} from "$lib/time/format.js";

import { formatTime } from "$lib/time/format.js";

export function formatAbsoluteDateTime(isoString) {
  return formatTime(isoString, { style: "exact" });
}

export function formatAbsoluteDate(isoString) {
  return formatTime(isoString, { style: "date" });
}

export function formatTimestamp(isoString, now = Date.now()) {
  return formatTime(isoString, { now, style: "relative" });
}
