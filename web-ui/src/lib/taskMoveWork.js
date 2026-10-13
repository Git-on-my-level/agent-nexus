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
  if (!work || !phase) throw new Error("Choose a task and destination.");
  if (phase === "done" && !resolutionRefs.length)
    throw new Error("Add evidence before marking this task done.");
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
      "The board revision is unavailable. Use latest version before sending this move.",
    );
  if (!Number.isSafeInteger(Number(work.version)) || Number(work.version) < 1)
    throw new Error(
      "The task revision is unavailable. Use latest version before sending this move.",
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
        if_version: Number(work.version),
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
