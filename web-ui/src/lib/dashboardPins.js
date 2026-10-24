/**
 * Dashboard pins as single operations rather than whole sets.
 *
 * Core only accepts a whole ordered set, so a set computed from a page's last
 * read drops whatever that read missed: a newer confirmation, another tab's
 * edit. A saved edit is instead one `{ op: "add" | "remove", ref }`, applied
 * at send time to a fresh read. Both operations are idempotent, so a replay
 * of the same edit lands on the same set.
 */

/** Core keeps at most this many personal pins (primitives/dashboard_pins.go). */
export const MAX_DASHBOARD_PINS = 12;

export const PINS_OPERATION = "workspace.dashboard.pins.set";

/** The reader's own pins from a pins read; the workspace default is not one. */
export const personalPins = (read) =>
  read &&
  read.source !== "workspace_default" &&
  Array.isArray(read.document_refs)
    ? read.document_refs
    : [];

/**
 * `refs` with one edit applied, and whether the edit had room. Adding a ref
 * already pinned, or removing one that is not, changes nothing.
 */
export function applyPinOp(refs, { op, ref }) {
  if (op === "add") {
    if (refs.includes(ref)) return { refs, changed: false, full: false };
    if (refs.length >= MAX_DASHBOARD_PINS)
      return { refs, changed: false, full: true };
    return { refs: [...refs, ref], changed: true, full: false };
  }
  if (op === "remove" && refs.includes(ref))
    return {
      refs: refs.filter((entry) => entry !== ref),
      changed: true,
      full: false,
    };
  return { refs, changed: false, full: false };
}

/** Saved order: the client order key, then the outbox key for a tie. */
export const comparePinEdits = (a, b) =>
  a.request.order - b.request.order ||
  (a.key < b.key ? -1 : a.key > b.key ? 1 : 0);

/**
 * Pin edits that may still reach core, oldest first. A definitive failure
 * (attention) is left out: the reader sees core's set and the failure.
 */
export const pendingPinEdits = (rows) =>
  (rows ?? [])
    .filter(
      (row) =>
        row.operation === PINS_OPERATION &&
        row.request?.op &&
        !["superseded", "attention"].includes(row.state),
    )
    .sort(comparePinEdits);

/** What the reader should see: core's set with saved edits applied in order. */
export const displayedPins = (base, rows) =>
  pendingPinEdits(rows).reduce(
    (refs, row) => applyPinOp(refs, row.request).refs,
    base,
  );

/**
 * Edits to one ref must reach core in the order they were made (add, then
 * remove, must not land as remove, then add). Edits to different refs
 * commute, so only an older sendable edit to the same ref holds one back.
 */
export const pinEditWaits = (entry, entries) =>
  entry.operation === PINS_OPERATION &&
  entries.some(
    (other) =>
      other !== entry &&
      other.key !== entry.key &&
      other.operation === PINS_OPERATION &&
      other.state !== "attention" &&
      other.request?.ref === entry.request?.ref &&
      comparePinEdits(other, entry) < 0,
  );
