/**
 * The only place a timestamp becomes words.
 *
 * Every surface reads an instant the same way, in the browser's locale and
 * timezone. Relative phrases stay in the product's compact English so a row,
 * a badge and a sentence cannot disagree; the calendar and the exact title
 * follow `Intl` and the reader's zone. Nothing here prints a UTC clock.
 *
 * Thresholds, compared with `now` on the local calendar:
 *
 * - under 45 seconds either way: "just now"
 * - under 60 minutes: "15 min ago" / "in 10 min" (whole minutes, at least 1)
 * - under 6 hours: "3 h ago" / "in 3 h", even across midnight
 * - the previous local day, once it is at least 6 hours away: "yesterday"
 * - the next local day, once it is at least 6 hours away: "tomorrow"
 * - otherwise a short local date: "Oct 5" in the same year, "Oct 5, 2025" in
 *   another
 *
 * `exact` is the hover and the accessible name: full local date and time
 * plus the short timezone name ("Oct 5, 2026, 1:56 PM GMT+7").
 * `clock` is a time of day ("1:56 PM") for a chat group or a morning stamp.
 * `date` is a calendar date with the year, for a fact where the hour is noise.
 * `elapsed` is a duration ("41m", "3h 12m"), not an instant.
 */

const JUST_NOW_MS = 45_000;
const MINUTE_MS = 60_000;
const HOUR_MS = 3_600_000;
const RECENT_HOURS = 6;

/**
 * @param {string|number|Date|null|undefined} value
 * @returns {number|null}
 */
export function instantMs(value) {
  if (value == null || value === "") return null;
  const then =
    value instanceof Date ? value.getTime() : new Date(value).getTime();
  return Number.isFinite(then) ? then : null;
}

/**
 * ISO 8601 for a `<time datetime>` attribute. Empty when there is no instant,
 * so a missing value cannot become the epoch.
 *
 * @param {string|number|Date|null|undefined} value
 */
export function instantIso(value) {
  const then = instantMs(value);
  if (then == null) return "";
  if (typeof value === "string" && /(?:Z|[+-]\d{2}:\d{2})$/.test(value.trim()))
    return value.trim();
  return new Date(then).toISOString();
}

function localDayNumber(date) {
  return Date.UTC(date.getFullYear(), date.getMonth(), date.getDate());
}

function calendarDaysApart(now, then) {
  return Math.round((localDayNumber(now) - localDayNumber(then)) / 86_400_000);
}

function phrase(count, unit, future) {
  return future ? `in ${count} ${unit}` : `${count} ${unit} ago`;
}

/**
 * @param {number} then
 * @param {number} now
 * @param {string|undefined} locale
 */
function formatRelative(then, now, locale) {
  const diff = now - then;
  const abs = Math.abs(diff);
  const future = diff < 0;
  if (abs < JUST_NOW_MS) return "just now";

  const minutes = Math.floor(abs / MINUTE_MS);
  if (minutes < 60) return phrase(Math.max(1, minutes), "min", future);

  const hours = Math.floor(abs / HOUR_MS);
  const days = calendarDaysApart(new Date(now), new Date(then));
  if (hours >= RECENT_HOURS && days === 1 && !future) return "yesterday";
  if (hours >= RECENT_HOURS && days === -1 && future) return "tomorrow";
  if (hours < 24) return phrase(Math.max(1, hours), "h", future);
  return formatShortDate(new Date(then), new Date(now), locale);
}

function formatShortDate(then, now, locale) {
  const sameYear = then.getFullYear() === now.getFullYear();
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    ...(sameYear ? {} : { year: "numeric" }),
  }).format(then);
}

function formatExact(then, locale) {
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
    timeZoneName: "short",
  }).format(then);
}

function formatClock(then, locale) {
  return new Intl.DateTimeFormat(locale, {
    hour: "numeric",
    minute: "2-digit",
  }).format(then);
}

function formatDateOnly(then, locale) {
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(then);
}

/**
 * @param {string|number|Date|null|undefined} value
 * @param {{
 *   now?: number,
 *   locale?: string,
 *   style?: "relative"|"exact"|"clock"|"date",
 * }} [options]
 * @returns {string}
 */
export function formatTime(value, options = {}) {
  const { now = Date.now(), locale, style = "relative" } = options;
  if (value == null || value === "") return "";
  const then = instantMs(value);
  if (then == null)
    return style === "relative" || style === "exact" ? String(value) : "";
  const date = new Date(then);
  if (style === "exact") return formatExact(date, locale);
  if (style === "clock") return formatClock(date, locale);
  if (style === "date") return formatDateOnly(date, locale);
  return formatRelative(then, Number(now), locale);
}

/**
 * A duration, not an instant: "41m", "3h 12m", "2d 4h". Under a minute is
 * "<1m". Empty for a missing or negative length.
 *
 * @param {number} ms
 */
export function formatElapsed(ms) {
  const value = Number(ms);
  if (!Number.isFinite(value) || value < 0) return "";
  const minutes = Math.floor(value / MINUTE_MS);
  if (minutes < 1) return "<1m";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const rest = minutes % 60;
    return rest ? `${hours}h ${rest}m` : `${hours}h`;
  }
  const days = Math.floor(hours / 24);
  const restHours = hours % 24;
  return restHours ? `${days}d ${restHours}h` : `${days}d`;
}

/**
 * Age of an observation counted in seconds at read time. "Age unknown" when
 * the count itself is missing — a blank would look like "just now".
 *
 * @param {number} seconds
 * @param {number} [now]
 */
export function formatAgeSeconds(seconds, now = Date.now()) {
  if (!Number.isFinite(seconds)) return "Age unknown";
  const age = Math.max(0, seconds);
  return formatTime(now - age * 1000, { now, style: "relative" });
}

const pad = (n) => String(n).padStart(2, "0");

/**
 * Value for `<input type="datetime-local">`, in the reader's zone.
 * Not a display string.
 *
 * @param {string|number|Date|null|undefined} iso
 */
export function isoToDatetimeLocal(iso) {
  const then = instantMs(iso);
  if (then == null) return "";
  const d = new Date(then);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * A datetime-local value (`YYYY-MM-DDTHH:MM`) back to ISO 8601.
 * Empty for a blank or unparseable value.
 *
 * @param {string|null|undefined} local
 */
export function datetimeLocalToIso(local) {
  if (!local) return "";
  const d = new Date(local);
  if (Number.isNaN(d.getTime())) return "";
  return d.toISOString();
}
