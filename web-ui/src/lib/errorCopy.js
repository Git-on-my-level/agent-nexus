/**
 * What a reader sees when a request fails.
 *
 * The core client writes its errors for logs: "Failed to load workspace:
 * anx-core request failed at same-origin: GET /threads/x/workspace (404) -
 * not found". The page's own lead-in and the server's reason are what an
 * operator needs; the transport, method, path and startup advice are
 * diagnostics that already sit in the network log, so they are dropped. The reason keeps the server's wording and case: it is quoted,
 * not paraphrased. Anything that is not a transport error passes through.
 */

const TRANSPORT =
  /^(?:(.*?):\s+)?anx-core request failed[^:]*:\s*[A-Z]+\s+\S+\s+\((\d{3}|unknown)\)(?:\s*-\s*(.*?))?(?:\s*\[?anx-core may be unavailable; verify backend startup and base URL\.\]?)?\s*$/s;
/** Startup advice for whoever runs the server; it reads as noise to anyone else. */
const GUIDANCE =
  /\s*\[?anx-core may be unavailable; verify backend startup and base URL\.\]?\s*$/;
const UNREACHABLE = /^(?:(.*?):\s+)?Unable to reach anx-core\b/s;

/** Reasons that only restate the status code. */
const STATUS_ECHOES = new Set([
  "not found",
  "forbidden",
  "unauthorized",
  "internal server error",
  "internal error",
  "bad gateway",
  "service unavailable",
  "gateway timeout",
  "unknown",
]);

function statusSentence(status) {
  if (status === 401) return "Your session has ended. Sign in again.";
  if (status === 403) return "You don't have access to this.";
  if (status === 404) return "This doesn't exist, or it was moved or removed.";
  if (status === 409) return "This changed while you were looking. Reload it.";
  if (status === 429) return "Too many requests. Try again in a moment.";
  if (status >= 500) return "The workspace server didn't answer. Try again.";
  return "This request didn't go through. Try again.";
}

function withLead(lead, body) {
  const head = String(lead ?? "").trim();
  return head ? `${head}: ${body}` : body;
}

/**
 * @param {unknown} raw an Error, or the message string a page already stored
 * @returns {string}
 */
export function describeError(raw) {
  const text = String(raw instanceof Error ? raw.message : (raw ?? "")).trim();
  if (!text) return "";

  const unreachable = text.match(UNREACHABLE);
  if (unreachable) {
    const body =
      "reach the workspace server. Check your connection and try again.";
    return unreachable[1]
      ? withLead(unreachable[1], `can't ${body}`)
      : `Can't ${body}`;
  }

  const match = text.match(TRANSPORT);
  if (!match) return text.replace(GUIDANCE, "") || text;

  const status = match[2] === "unknown" ? 0 : Number(match[2]);
  const reason = (match[3] ?? "").trim();
  const body =
    reason && !STATUS_ECHOES.has(reason.toLowerCase())
      ? reason
      : statusSentence(status);
  return withLead(match[1], body);
}
