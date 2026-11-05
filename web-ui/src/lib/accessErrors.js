/**
 * The text to show for a failed Access action. Core's reason (`details`)
 * reads better than the transport-level message around it.
 *
 * @param {unknown} error
 * @param {string} fallback
 */
export function accessErrorMessage(error, fallback) {
  if (!error) return fallback;
  if (typeof error === "string") return error || fallback;
  const details = /** @type {{ details?: unknown }} */ (error).details;
  if (typeof details === "string" && details.trim()) return details;
  if (error instanceof Error) return error.message || fallback;
  return fallback;
}
