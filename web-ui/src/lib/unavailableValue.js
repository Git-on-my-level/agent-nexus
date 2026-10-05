/**
 * Values that are not there.
 *
 * A metric tile is sized for `94%`. Put the word "unknown" in it at 40px and
 * it wraps down the tile, pushes the sparkline out of its box and shoves
 * everything below it off the grid — so an unread metric did more damage to
 * the page than a wrong one.
 *
 * The fact a reader needs is "no number here", and an em dash says that in one
 * glyph. *Why* is a tooltip, which costs no width at all.
 *
 * This decides what counts as absent. It is deliberately a short list: a real
 * reading of `0`, `false` or `"green"` is a value and must render as itself.
 */

/**
 * Words a producer uses for an absence. Matched case-insensitively after
 * trimming; anything else is a real value.
 */
const ABSENT_WORDS = new Set([
  "unknown",
  "unavailable",
  "not available",
  "not established",
  "n/a",
  "na",
  "none",
  "null",
  "undefined",
  "no data",
  "no value",
  "no recent reading",
  "-",
  "--",
  "—",
  "?",
]);

const asText = (value) => String(value ?? "").trim();

/**
 * Is this value an absence?
 *
 * @param {unknown} value
 */
export function isUnavailableValue(value) {
  if (value === null || value === undefined) return true;
  if (typeof value === "number") return !Number.isFinite(value);
  if (typeof value === "boolean") return false;
  const text = asText(value);
  if (!text) return true;
  return ABSENT_WORDS.has(text.toLowerCase());
}

/**
 * A metric, ready to render.
 *
 * @param {unknown} value
 * @param {{ reason?: string, unit?: string }} [options]
 *   `reason` is what the caller knows about the gap — a panel's own
 *   unavailability message, say. Without one the reason is read off the value:
 *   a producer that said "unknown" said something, and quoting it back is more
 *   use to whoever has to fix it than a generic line.
 * @returns {{ available: boolean, text: string, reason: string }}
 */
export function metricValue(value, { reason = "", unit = "" } = {}) {
  if (isUnavailableValue(value)) {
    const given = asText(value);
    const why =
      asText(reason) ||
      (given ? `Reported as “${given}”.` : "No value reported.");
    return { available: false, text: "", reason: why };
  }
  const unitText = asText(unit);
  return {
    available: true,
    text: unitText ? `${value} ${unitText}` : String(value),
    reason: "",
  };
}
