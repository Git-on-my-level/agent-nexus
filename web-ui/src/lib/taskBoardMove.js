import {
  cardIdFromWork,
  isNexusOwned,
  label,
  sourceLabel,
  workKey,
  workTargetRevision,
} from "./pm/presentation.js";

export function phaseRequestKey(work, phase) {
  return `task-phase:${workKey(work)}:${phase}`;
}

const STATUS_CHANGE_PREFIX = "request status change";

export function statusChangeInstruction(work, phase) {
  const source = sourceLabel(work?.source);
  return `${STATUS_CHANGE_PREFIX} at ${source} to ${label(phase)}`;
}

export function createStatusChangeDecisionPayload(work, phase) {
  return {
    instruction: statusChangeInstruction(work, phase),
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
export function requestedDecisionMap(decisions = [], records = []) {
  const keyByRef = new Map(
    records
      .filter((work) => work?.ref)
      .map((work) => [work.ref, workKey(work)]),
  );
  const map = {};
  for (const decision of decisions) {
    if (!decision || decision.status !== "awaiting_answer") continue;
    const phaseRequest =
      decision.scope === "work.phase" ||
      String(decision.instruction || "").startsWith(STATUS_CHANGE_PREFIX);
    if (!phaseRequest) continue;
    const key = keyByRef.get(decision.work_ref);
    if (key) map[key] = decision.id;
  }
  return map;
}

/**
 * Apply a board drop. Nexus-owned tasks move via cards.move.
 * Source-owned tasks never mutate the source: propose a PM decision instead.
 *
 * @returns {Promise<{ kind: "moved" | "requested", work: object, decision?: object }>}
 */
export async function applyTaskPhaseMove(coreClient, work, phase) {
  if (!work || !phase || (work.phase || "unknown") === phase) {
    return { kind: "noop", work };
  }
  if (isNexusOwned(work)) {
    const cardId = cardIdFromWork(work);
    const boardId = String(work.board_ref || work.board_id || "").trim();
    await coreClient.moveBoardCard(boardId, cardId, { column_key: phase });
    return { kind: "moved", work: { ...work, phase } };
  }
  const decision = await coreClient.createPmDecision(
    createStatusChangeDecisionPayload(work, phase),
  );
  return { kind: "requested", work, decision };
}
