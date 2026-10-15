import { expect, it, vi } from "vitest";
vi.mock("../../src/lib/mutationOutbox.js", () => ({
  enqueueLocalMutation: vi.fn(),
}));
import { taskMoveDraft, taskMoveRequest } from "../../src/lib/taskMoveWork.js";
const work = {
  ref: "card:one",
  board_ref: "board:one",
  version: 7,
  phase: "review",
  source: { authority: "nexus" },
};
it("freezes original bases, end anchor and completion evidence", () => {
  const options = {
    boardToken: "original",
    beforeCardId: "",
    resolutionRefs: ["artifact:proof"],
  };
  const draft = taskMoveDraft(work, "done", options);
  options.resolutionRefs.push("artifact:new");
  work.version = 8;
  expect(taskMoveRequest(draft, "human", "replay").request.body).toEqual({
    actor_id: "human",
    request_key: "replay",
    column_key: "done",
    if_board_updated_at: "original",
    if_version: 7,
    before_card_id: "",
    resolution: "done",
    resolution_refs: ["artifact:proof"],
  });
  work.version = 7;
});
it("keeps source intent and receipt keys separate and actor bound", () => {
  const draft = taskMoveDraft(
    {
      ...work,
      source: { authority: "github", revision: "old" },
      decision_revision: "old",
    },
    "done",
    { resolutionRefs: ["event:proof"] },
  );
  const sent = taskMoveRequest(draft, "original-human", "receipt");
  expect(sent.operation).toBe("pm.decisions.create");
  expect(sent.request.body).toMatchObject({
    actor_id: "original-human",
    replay_key: "receipt",
    request_key: "task-phase:card:one:done:old",
    target_revision: "old",
    payload: { phase: "done", resolution_refs: ["event:proof"] },
  });
});
it("fails closed without task and board bases or completion evidence", () => {
  expect(() =>
    taskMoveRequest(taskMoveDraft(work, "review"), "human", "key"),
  ).toThrow(/initiative changed/);
  expect(() =>
    taskMoveRequest(
      taskMoveDraft({ ...work, version: undefined }, "review", {
        boardToken: "stamp",
      }),
      "human",
      "key",
    ),
  ).toThrow(/initiative changed/);
  expect(() =>
    taskMoveRequest(
      taskMoveDraft(work, "done", { boardToken: "stamp" }),
      "human",
      "key",
    ),
  ).toThrow(/evidence/);
});
