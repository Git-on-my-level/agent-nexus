/**
 * Presentation helpers for the PM conversation thread.
 *
 * Pure functions only: no network, no core policy, no durable state. Core owns
 * turn status; this module decides how a turn reads on screen.
 */

import { decisionIdsFromTurn } from "./turnDecisions.js";

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

/**
 * A PM record id as the runner writes it in prose: `pm_` plus 32 lowercase hex.
 * The leading capture keeps the character before the id so an id that is
 * already part of a ref (`decision:pm_…`) or of a link target (`/inbox?…/pm_…`)
 * is left alone.
 */
const BARE_PM_ID_PATTERN =
  /(^|[^A-Za-z0-9_:/[-])(pm_[0-9a-f]{32})(?![0-9A-Za-z_-])/g;

/** Fenced blocks and inline code spans: prose transforms must not reach inside. */
const CODE_SEGMENT_PATTERN = /(```[\s\S]*?```|~~~[\s\S]*?~~~|`[^`\n]*`)/g;

/** A decision record carries one of these; anything else is not a decision. */
const DECISION_STATUSES = new Set([
  "awaiting_answer",
  "answered",
  "superseded",
]);

/**
 * Bare `pm_…` ids named in a text. The runner names its own proposals this way
 * — as an id in a sentence, with no `decision:` prefix — so these are only
 * candidates: an id here may be a decision, a turn, a conversation, or a record
 * this actor cannot read. Resolving them is the caller's job.
 *
 * @param {unknown} text
 * @returns {string[]} unique ids in first-seen order
 */
export function bareDecisionIds(text) {
  const seen = new Set();
  const ids = [];
  for (const match of String(text ?? "").matchAll(BARE_PM_ID_PATTERN)) {
    const id = match[2];
    if (seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}

/**
 * Candidate decision ids for a turn: bare ids in the reply that are not already
 * recorded as `decision:` refs.
 *
 * @param {{ evidence_refs?: string[], response?: string } | null} turn
 * @returns {string[]}
 */
export function candidateDecisionIdsFromTurn(turn) {
  const confirmed = new Set(decisionIdsFromTurn(turn));
  return bareDecisionIds(turn?.response).filter((id) => !confirmed.has(id));
}

/**
 * Whether a record fetched for a candidate id really is a decision. A lookup
 * that failed is stored as `null`; a lookup that answered with some other kind
 * of PM record must not become a "Proposed decision" row.
 *
 * @param {unknown} record
 * @param {string} id
 */
export function isDecisionRecord(record, id = "") {
  if (!record || typeof record !== "object" || Array.isArray(record))
    return false;
  const recordId = String(record.id ?? "").trim();
  if (id && recordId && recordId !== String(id)) return false;
  if (typeof record.instruction === "string" && record.instruction.trim())
    return true;
  return DECISION_STATUSES.has(String(record.status ?? ""));
}

/**
 * Decision ids to show under a reply: every `decision:` ref (shown even while
 * its record is still loading, because the ref itself is the runner's claim),
 * plus every bare candidate that resolved to a real decision.
 *
 * @param {{ evidence_refs?: string[], response?: string } | null} turn
 * @param {Record<string, unknown>} records id → fetched record, `null` on failure
 * @returns {string[]}
 */
export function proposedDecisionIds(turn, records = {}) {
  const ids = decisionIdsFromTurn(turn);
  const seen = new Set(ids);
  for (const id of candidateDecisionIdsFromTurn(turn)) {
    if (seen.has(id) || !isDecisionRecord(records?.[id], id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}

/**
 * Short label for an id standing in the middle of a sentence. The full id stays
 * on the link title.
 *
 * @param {string} id
 */
export function decisionChipLabel(id) {
  const value = String(id ?? "");
  const short = value.startsWith("pm_")
    ? value.slice(0, "pm_".length + 8)
    : value.slice(0, 8);
  return `Decision ${short}`;
}

function linkifySegment(segment, hrefFor) {
  return segment.replace(BARE_PM_ID_PATTERN, (match, lead, id) => {
    const href = String(hrefFor(id) ?? "").trim();
    if (!href) return match;
    return `${lead}[${decisionChipLabel(id)}](${href} "${id}")`;
  });
}

/**
 * The reply body with each resolved bare id replaced by a short inline link, so
 * a paragraph reads as a sentence instead of as a wall of hex. `hrefFor`
 * returns an empty string for an id that is not an answerable decision, and
 * that id is left exactly as the runner wrote it. Code spans are never touched.
 *
 * @param {unknown} text
 * @param {(id: string) => string} hrefFor
 * @returns {string}
 */
export function linkifyDecisionIds(text, hrefFor) {
  const source = String(text ?? "");
  if (!source || typeof hrefFor !== "function") return source;
  return source
    .split(CODE_SEGMENT_PATTERN)
    .map((segment, index) =>
      index % 2 === 1 ? segment : linkifySegment(segment, hrefFor),
    )
    .join("");
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
