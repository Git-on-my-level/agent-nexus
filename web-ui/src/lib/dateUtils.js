export function parseTimestampMs(value) {
  if (!value) {
    return Number.NaN;
  }

  const parsed = Date.parse(String(value));
  return Number.isFinite(parsed) ? parsed : Number.NaN;
}
