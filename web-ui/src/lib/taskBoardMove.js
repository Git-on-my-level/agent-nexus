import { proposalVoidReason } from "./inboxMailbox.js";
import {
  isNexusOwned,
  label,
  sourceLabel,
  workKey,
  workTargetRevision,
} from "./pm/presentation.js";

export function phaseRequestKey(work, phase) {
  // The revision is part of the key: a request made after the work moved is
  // a new request, not a replay of the stale one.
  return `task-phase:${workKey(work)}:${phase}:${workTargetRevision(work)}`;
}

const STATUS_CHANGE_PREFIX = "request status change";

export function statusChangeInstruction(work, phase) {
  const source = sourceLabel(work?.source);
  return `${STATUS_CHANGE_PREFIX} at ${source} to ${label(phase)}`;
}

export function createStatusChangeDecisionPayload(
  work,
  phase,
  { resolutionRefs = [] } = {},
) {
  return {
    instruction: statusChangeInstruction(work, phase),
    // The target phase travels as structured data; the instruction is prose
    // for the reader and never the thing core executes.
    payload:
      phase === "done" && resolutionRefs.length
        ? { phase, resolution_refs: resolutionRefs }
        : { phase },
    request_key: phaseRequestKey(work, phase),
    scope: "work.phase",
    target_revision: workTargetRevision(work),
    work_ref: work.ref || workKey(work),
  };
}

/**
 * Map workKey -> decision id for pending phase-change decisions, so the
 * Requested badge can deep-link to the Inbox item awaiting an answer.
 * Awaiting decisions only; phase scope or a status-change instruction;
 * only for refs that match a tracked work.
 */
export function requestedDecisionMap(
  decisions = [],
  records = [],
  actions = [],
) {
  // An answered request whose action reached a terminal or acknowledged
  // state is no longer live on the board.
  const closedDecisions = new Set(
    actions
      .filter((action) =>
        ["acknowledged", "verified", "failed"].includes(
          String(action?.status ?? ""),
        ),
      )
      .map((action) => action.decision_id),
  );
  const keyByRef = new Map(
    records
      .filter((work) => work?.ref)
      .map((work) => [work.ref, workKey(work)]),
  );
  const workByRef = new Map(
    records.filter((work) => work?.ref).map((work) => [work.ref, work]),
  );
  const externalRefs = new Set(
    records
      .filter((work) => work?.ref && !isNexusOwned(work))
      .map((w) => w.ref),
  );
  const map = {};
  for (const decision of decisions) {
    if (!decision) continue;
    // Awaiting anywhere; answered only for source-owned work, where the
    // request stays live until a delivery path exists.
    const work = workByRef.get(decision.work_ref) || null;
    const live =
      (decision.status === "awaiting_answer" &&
        !proposalVoidReason(decision, work)) ||
      (decision.status === "answered" &&
        externalRefs.has(decision.work_ref) &&
        !closedDecisions.has(decision.id));
    if (!live) continue;
    const phaseRequest =
      decision.scope === "work.phase" ||
      String(decision.instruction || "").startsWith(STATUS_CHANGE_PREFIX);
    if (!phaseRequest) continue;
    const key = keyByRef.get(decision.work_ref);
    if (!key) continue;
    // An awaiting request outranks an answered one for the same task.
    if (decision.status === "answered" && map[key]) continue;
    map[key] = decision.id;
  }
  return map;
}
