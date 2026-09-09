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

export function statusChangeInstruction(work, phase) {
  const source = sourceLabel(work?.source);
  return `request status change at ${source} to ${label(phase)}`;
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
