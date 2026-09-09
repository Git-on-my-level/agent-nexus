/**
 * Presentation helpers for the PM conversation thread.
 *
 * Pure functions only: no network, no core policy, no durable state. Core owns
 * turn status; this module decides how a turn reads on screen.
 */

/**
 * Typed refs the PM writes in prose. `url:` is deliberately absent — a URL
 * carries its own colons and slashes and would not survive this scan intact.
 */
const TYPED_REF_PATTERN =
  /\b(?:card|work|document|document_revision|card_revision|thread|inbox|decision):[A-Za-z0-9._~@+-]+/g;

/** Sentence punctuation trailing a ref (`card:release.`) is not part of the ref. */
function trimRefPunctuation(ref) {
  return String(ref).replace(/[.,;:!?)\]}"'-]+$/, "");
}

/**
 * Typed refs mentioned in the body of a PM reply. The runner records some refs
 * as `evidence_refs`, but names most of them only in prose; those are the ones
 * a reader actually wants to click.
 *
 * @param {unknown} text
 * @returns {string[]}
 */
export function refsFromResponse(text) {
  const matches = String(text ?? "").match(TYPED_REF_PATTERN) || [];
  return matches
    .map(trimRefPunctuation)
    .filter((ref) => /^[a-z_]+:.+/.test(ref));
}

/**
 * Every ref worth showing as a chip under an answer: recorded evidence plus
 * refs named in the reply. `decision:` refs are excluded — they render as
 * answerable rows in the "Proposed decisions" block instead.
 *
 * @param {{ evidence_refs?: string[], response?: string } | null} turn
 * @returns {string[]} unique refs in first-seen order
 */
export function evidenceRefsForTurn(turn) {
  const refs = [
    ...(turn?.evidence_refs || []),
    ...refsFromResponse(turn?.response),
  ];
  const seen = new Set();
  const ordered = [];
  for (const candidate of refs) {
    const ref = String(candidate ?? "").trim();
    if (!ref || ref.startsWith("decision:") || seen.has(ref)) continue;
    seen.add(ref);
    ordered.push(ref);
  }
  return ordered;
}

/** A new time-group header appears when a turn opens more than this after the last one. */
export const TIME_GROUP_GAP_MS = 5 * 60 * 1000;

/** Waiting longer than this without a reply is worth naming as a possible dead runner. */
export const STALLED_AFTER_MS = 45_000;

/**
 * Whether `turn` opens a new time group. Unparseable timestamps never open a
 * group except at the head of the thread — a missing time is not a gap.
 *
 * @param {{ created_at?: string } | null} turn
 * @param {{ created_at?: string } | null} previous
 */
export function startsTimeGroup(turn, previous) {
  if (!previous) return true;
  const at = Date.parse(turn?.created_at ?? "");
  const before = Date.parse(previous?.created_at ?? "");
  if (!Number.isFinite(at) || !Number.isFinite(before)) return false;
  return at - before > TIME_GROUP_GAP_MS;
}

/**
 * Local wall-clock time (`14:32`) for a group header. Empty when the instant is
 * missing or unparseable, so the caller can drop the header entirely.
 *
 * @param {unknown} iso
 */
export function clockTime(iso) {
  if (!iso) return "";
  const date = new Date(String(iso));
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

/**
 * Elapsed wait as `42s` / `1m 42s` / `2h 05m`. Empty for an unknown duration.
 *
 * @param {number} ms
 */
export function elapsedLabel(ms) {
  if (!Number.isFinite(ms) || ms < 0) return "";
  const total = Math.floor(ms / 1000);
  const seconds = total % 60;
  const minutes = Math.floor(total / 60) % 60;
  const hours = Math.floor(total / 3600);
  if (hours) return `${hours}h ${String(minutes).padStart(2, "0")}m`;
  if (minutes) return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
  return `${seconds}s`;
}

/**
 * How the PM half of a turn should read. A transient state never wears a badge:
 * `pending` is a live row, `failed` and `unknown` are a sentence plus a retry.
 *
 * @param {{ status?: string, response?: string, failure?: string, created_at?: string } | null} turn
 * @param {number} now
 * @returns {{ kind: "answered"|"pending"|"failed"|"unknown", elapsed: string, stalled: boolean, detail: string }}
 */
export function turnState(turn, now = Date.now()) {
  const base = { elapsed: "", stalled: false, detail: "" };
  if (turn?.response) return { ...base, kind: "answered" };
  const status = String(turn?.status ?? "");
  if (status === "failed")
    return {
      ...base,
      kind: "failed",
      detail: String(turn?.failure ?? "").trim(),
    };
  if (status === "unknown") return { ...base, kind: "unknown" };
  const created = Date.parse(turn?.created_at ?? "");
  const waited = Number.isFinite(created) ? now - created : Number.NaN;
  return {
    ...base,
    kind: "pending",
    elapsed: elapsedLabel(waited),
    stalled: Number.isFinite(waited) && waited >= STALLED_AFTER_MS,
  };
}

/**
 * Whether any turn is still waiting on the PM — the only reason to run a
 * one-second clock.
 *
 * @param {Array<object>} turns
 */
export function hasPendingTurn(turns, now = Date.now()) {
  return (turns || []).some((turn) => turnState(turn, now).kind === "pending");
}

/**
 * Stick-to-bottom test. A detached container counts as "at the bottom" so a
 * thread that has not laid out yet still anchors to the newest turn.
 *
 * @param {{ scrollHeight?: number, scrollTop?: number, clientHeight?: number } | null} element
 * @param {number} threshold
 */
export function isNearBottom(element, threshold = 64) {
  if (!element) return true;
  const distance =
    Number(element.scrollHeight ?? 0) -
    Number(element.scrollTop ?? 0) -
    Number(element.clientHeight ?? 0);
  if (!Number.isFinite(distance)) return true;
  return distance < threshold;
}
