import { get } from "svelte/store";
import { localWorkScope } from "$lib/localWorkScope.js";
import { enqueueLocalMutation } from "$lib/mutationOutbox.js";
import { createStatusChangeDecisionPayload } from "$lib/taskBoardMove.js";
import {
  cardIdFromWork,
  isNexusOwned,
  label,
  workKey,
} from "$lib/pm/presentation.js";

export const taskMoveTarget = (work) => `task:${workKey(work)}:move`;
export const isTaskMove = (entry) =>
  ["cards.move", "pm.decisions.create"].includes(entry.operation);

// A complete immutable intent. Reads and retries may never replace this base.
export function taskMoveDraft(work, phase, options = {}) {
  return JSON.parse(
    JSON.stringify({
      work,
      phase,
      boardToken: "",
      resolutionRefs: [],
      ...options,
    }),
  );
}

export function taskMoveRequest(draft, actor, key) {
  const { work, phase, boardToken, resolutionRefs = [] } = draft;
  if (!work || !phase) throw new Error("Choose an initiative and destination.");
  if (phase === "done" && !resolutionRefs.length)
    throw new Error("Add evidence before marking this initiative done.");
  if (!isNexusOwned(work))
    return {
      operation: "pm.decisions.create",
      request: {
        body: {
          ...createStatusChangeDecisionPayload(work, phase, { resolutionRefs }),
          actor_id: actor,
          replay_key: key,
        },
      },
    };
  const board = work.board_ref || work.board_id;
  if (!board || !boardToken)
    throw new Error(
      "This initiative changed. Use the latest version before moving it.",
    );
  // A card with no work_metadata row is still Nexus-owned. GetWork reports
  // version 0, and the move endpoint accepts that fence.
  const version = Number(work.version);
  if (!Number.isSafeInteger(version) || version < 0)
    throw new Error(
      "This initiative changed. Use the latest version before moving it.",
    );
  return {
    operation: "cards.move",
    request: {
      board,
      id: cardIdFromWork(work),
      body: {
        actor_id: actor,
        request_key: key,
        column_key: phase,
        if_board_updated_at: boardToken,
        if_version: version,
        ...(Object.hasOwn(draft, "beforeCardId")
          ? { before_card_id: draft.beforeCardId }
          : {}),
        ...(phase === "done"
          ? { resolution: "done", resolution_refs: resolutionRefs }
          : {}),
      },
    },
  };
}

export function taskMoveError(error) {
  const reason = error instanceof Error ? error.message.trim() : "";
  return reason
    ? `Couldn't save that move. ${reason}`
    : "Couldn't save that move. Try again.";
}

export function enqueueTaskMove(draft, options = {}) {
  const key = crypto.randomUUID();
  const actor = JSON.parse(get(localWorkScope))[3];
  return enqueueLocalMutation({
    ...options,
    ...taskMoveRequest(draft, actor, key),
    key,
    target: taskMoveTarget(draft.work),
    draft,
    title: `Move “${draft.work.title}” to ${label(draft.phase)}`,
    preview: `${draft.work.title}\nDestination: ${label(draft.phase)}${draft.resolutionRefs?.length ? `\nEvidence: ${draft.resolutionRefs.join(", ")}` : ""}`,
  });
}
