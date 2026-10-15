import { expect, it, vi } from "vitest";

import {
  createWatchMover,
  displayedPlacement,
  watchMoveAttention,
} from "../../src/lib/watchMove.js";

const row = {
  ref: "card:ship",
  title: "Ship the brief",
  placement: "backlog",
};
const work = {
  ref: "card:ship",
  title: "Ship the brief",
  source: { authority: "nexus" },
  board_ref: "board:portfolio",
  version: 0,
};

function mover(overrides = {}) {
  const readWork = vi.fn(async () => ({ work, boardToken: "tok" }));
  const enqueue = vi.fn(async (draft) => ({ key: "new", draft }));
  const supersede = vi.fn(async () => true);
  const api = createWatchMover({
    readWork,
    enqueue,
    entries: () => [],
    supersede,
    confirmExternal: () => true,
    ...overrides,
  });
  return { ...api, readWork, enqueue, supersede };
}

it("sends no positional anchor", async () => {
  const { move, enqueue } = mover();
  const result = await move(row, "ready");
  expect(result).toEqual({ status: "queued", placement: "ready" });
  expect(enqueue.mock.calls[0][0].beforeCardId).toBe("");
  expect(enqueue.mock.calls[0][0].phase).toBe("ready");
});

it("lets the latest destination win when moves overlap", async () => {
  const resolvers = [];
  const readWork = vi.fn(
    () => new Promise((resolve) => resolvers.push(resolve)),
  );
  const { move, enqueue } = mover({ readWork });
  const first = move(row, "ready");
  const second = move(row, "review");
  expect(resolvers).toHaveLength(1);
  resolvers[0]({ work, boardToken: "tok" });
  await vi.waitFor(() => expect(resolvers).toHaveLength(2));
  resolvers[1]({ work, boardToken: "tok" });
  await expect(first).resolves.toEqual({
    status: "queued",
    placement: "review",
  });
  await expect(second).resolves.toEqual({
    status: "queued",
    placement: "review",
  });
  expect(enqueue).toHaveBeenCalledTimes(1);
  expect(enqueue.mock.calls[0][0].phase).toBe("review");
  expect(enqueue.mock.calls[0][0].beforeCardId).toBe("");
});

it("replaces an attention entry instead of ignoring the next move", async () => {
  const attention = {
    key: "old",
    target: "task:card:ship:move",
    state: "attention",
    operation: "cards.move",
    failure: {
      message:
        "This move needs a correction. Your destination and evidence are saved.",
    },
    draft: { phase: "ready" },
    request: { body: { before_card_id: "card:peer", column_key: "ready" } },
  };
  const { move, enqueue, supersede } = mover({ entries: () => [attention] });
  const result = await move(row, "in_progress");
  expect(result.status).toBe("queued");
  expect(result.placement).toBe("in_progress");
  expect(enqueue).toHaveBeenCalledTimes(1);
  expect(enqueue.mock.calls[0][0].beforeCardId).toBe("");
  expect(supersede).toHaveBeenCalledWith("old", "new");
  expect(watchMoveAttention([attention], [row.ref])).toBe(
    "This move needs a correction. Your destination and evidence are saved.",
  );
});

it("keeps a local column until the outbox confirms or the move fails", () => {
  const pending = {
    target: "task:card:ship:move",
    state: "pending",
    operation: "cards.move",
    draft: { phase: "ready" },
  };
  expect(displayedPlacement(row, [], {}, "ready")).toBe("ready");
  expect(displayedPlacement(row, [pending], {}, "")).toBe("ready");
  expect(
    displayedPlacement(
      row,
      [{ ...pending, state: "attention", failure: { message: "No." } }],
      {},
      "ready",
    ),
  ).toBe("");
  expect(
    displayedPlacement(row, [], {
      "task:card:ship:move": {
        operation: "cards.move",
        state: "sent",
        draft: { phase: "review" },
      },
    }),
  ).toBe("review");
  expect(
    displayedPlacement({ ...row, placement: "review" }, [], {
      "task:card:ship:move": {
        operation: "cards.move",
        state: "sent",
        draft: { phase: "review" },
      },
    }),
  ).toBe("");
});
