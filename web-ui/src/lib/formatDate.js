/**
 * Format an ISO 8601 timestamp as a human-readable relative or absolute date.
 * Returns "" for null/undefined inputs; returns the raw value if not parseable.
 */
export function formatTimestamp(isoString) {
  if (!isoString) return "";
  const date = new Date(isoString);
  if (isNaN(date.getTime())) return String(isoString);

  const now = new Date();
  const diffMs = now - date;
  const absDiffMs = Math.abs(diffMs);
  const absDiffSec = Math.floor(absDiffMs / 1000);
  const absDiffMin = Math.floor(absDiffSec / 60);
  const absDiffHour = Math.floor(absDiffMin / 60);
  const absDiffDay = Math.floor(absDiffHour / 24);

  const isFuture = diffMs < 0;

  if (absDiffSec < 60) return isFuture ? "in a moment" : "just now";
  if (absDiffMin < 60)
    return isFuture ? `in ${absDiffMin}m` : `${absDiffMin}m ago`;
  if (absDiffHour < 24)
    return isFuture ? `in ${absDiffHour}h` : `${absDiffHour}h ago`;
  if (absDiffDay < 7)
    return isFuture ? `in ${absDiffDay}d` : `${absDiffDay}d ago`;

  return date.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

/**
 * Convert an ISO 8601 string to the value expected by <input type="datetime-local">.
 * Outputs in the user's local timezone.
 */
export function isoToDatetimeLocal(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "";
  const pad = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * Convert a datetime-local input value (YYYY-MM-DDTHH:MM) back to ISO 8601.
 * Returns "" for empty/invalid values.
 */
export function datetimeLocalToIso(local) {
  if (!local) return "";
  const d = new Date(local);
  if (isNaN(d.getTime())) return "";
  return d.toISOString();
}
