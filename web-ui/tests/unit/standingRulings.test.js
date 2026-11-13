import { beforeEach, describe, expect, it, vi } from "vitest";

const queue = vi.hoisted(() => ({ listeners: [] }));
vi.mock("$lib/inboxResponseQueue.js", () => ({
  onInboxResponseCommitted: (listener) => {
    queue.listeners.push(listener);
    return () => queue.listeners.splice(queue.listeners.indexOf(listener), 1);
  },
}));
const outbox = await vi.hoisted(async () => {
  const { writable } = await import("svelte/store");
  return {
    enqueueLocalMutation: vi.fn(async (entry) => entry),
    mutationConfirmations: writable({}),
  };
});
vi.mock("$lib/mutationOutbox.js", () => outbox);
const brief = vi.hoisted(() => ({ refreshBrief: vi.fn(async () => {}) }));
vi.mock("$lib/briefStore.js", () => brief);

import {
  NOT_A_DECISION_REASON,
  canRuleOn,
  enqueueNotADecision,
  intend,
  notADecisionTarget,
  resetStandingAnswers,
  sentBackItemIds,
  standingAnswerIntent,
  watchStandingAnswers,
} from "../../src/lib/standingRulings.js";
import {
  NOT_A_DECISION,
  RULING_CREATE,
  RULING_REVOKE,
  sendStandingMutation,
} from "../../src/lib/standingRulingSend.js";

const ask = { kind: "ask", eventRef: "event:ask-91", title: "Merge?" };

beforeEach(() => {
  resetStandingAnswers();
  outbox.enqueueLocalMutation.mockClear();
  outbox.mutationConfirmations.set({});
  brief.refreshBrief.mockClear();
});

describe("which decisions can be ruled on", () => {
  it("is a single ask or review backed by an ask event", () => {
    expect(canRuleOn(ask)).toBe(true);
    expect(canRuleOn({ ...ask, kind: "review" })).toBe(true);
    for (const other of [
      { ...ask, eventRef: "" },
      { ...ask, kind: "access" },
      { ...ask, kind: "host" },
      { ...ask, kind: "pm_proposal" },
      { ...ask, batch: { count: 2 } },
      { ...ask, curation: { action: "merge" } },
      { ...ask, pmDecision: { id: "d1" } },
      { ...ask, accessRequestId: "a1" },
    ])
      expect(canRuleOn(other)).toBe(false);
  });
});

describe("not a decision", () => {
  it("saves a retire with the fixed reason and the ask as evidence", async () => {
    await enqueueNotADecision({
      itemId: "inbox:ask-91",
      askRef: "event:ask-91",
      always: true,
      title: "Merge?",
      href: "/decide/x",
    });
    const entry = outbox.enqueueLocalMutation.mock.calls[0][0];
    expect(entry).toMatchObject({
      target: notADecisionTarget("inbox:ask-91"),
      operation: NOT_A_DECISION,
      preview: "Merge?",
      request: {
        ask_ref: "event:ask-91",
        always: true,
        body: { reason: NOT_A_DECISION_REASON, evidence: ["event:ask-91"] },
      },
    });
    expect(entry.request.body.request_key).toBe(entry.key);
  });

  it("retires, then rules only when the reader said always", async () => {
    const client = {
      asksRetire: vi.fn(async () => ({ status: "withdrawn_as_stale" })),
      rulingsCreate: vi.fn(async () => ({ ruling: { id: "r1" } })),
    };
    const request = { ask_ref: "event:a", body: { request_key: "k" } };
    expect(
      await sendStandingMutation(client, {
        operation: NOT_A_DECISION,
        request: { ...request, always: false },
      }),
    ).toEqual({ retired: true });
    expect(client.rulingsCreate).not.toHaveBeenCalled();
    expect(
      await sendStandingMutation(client, {
        operation: NOT_A_DECISION,
        request: { ...request, always: true },
      }),
    ).toEqual({ retired: true, ruling: { id: "r1" } });
    expect(client.asksRetire).toHaveBeenLastCalledWith("event:a", {
      request_key: "k",
    });
    expect(client.rulingsCreate).toHaveBeenCalledWith({
      ask_ref: "event:a",
      kind: "not_a_decision",
    });
  });

  it("hides sent-back rows until a send needs the reader", () => {
    const target = notADecisionTarget("inbox:ask-91");
    const row = { operation: NOT_A_DECISION, target, state: "pending" };
    expect(sentBackItemIds([row], {})).toEqual(["inbox:ask-91"]);
    expect(sentBackItemIds([{ ...row, state: "attention" }], {})).toEqual([]);
    expect(sentBackItemIds([], { [target]: {}, "pm-decision:1": {} })).toEqual([
      "inbox:ask-91",
    ]);
  });

  it("reads the brief again once core confirms", () => {
    watchStandingAnswers();
    outbox.mutationConfirmations.set({ "pm-decision:1": {} });
    expect(brief.refreshBrief).not.toHaveBeenCalled();
    const target = notADecisionTarget("inbox:ask-91");
    outbox.mutationConfirmations.set({ [target]: {} });
    outbox.mutationConfirmations.set({ [target]: {} });
    expect(brief.refreshBrief).toHaveBeenCalledTimes(1);
  });
});

describe("always answer this way", () => {
  it("makes the ruling only after the answer is committed", async () => {
    watchStandingAnswers();
    intend("inbox:ask-91", true, ask, "Merge");
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
    queue.listeners[0]("inbox:other", {});
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
    queue.listeners[0]("inbox:ask-91", {});
    await Promise.resolve();
    expect(outbox.enqueueLocalMutation).toHaveBeenCalledTimes(1);
    expect(outbox.enqueueLocalMutation.mock.calls[0][0]).toMatchObject({
      operation: RULING_CREATE,
      request: {
        body: { ask_ref: "event:ask-91", kind: "answer", answer: "Merge" },
      },
    });
    // The intent is spent: a later commit of the same item makes nothing.
    queue.listeners[0]("inbox:ask-91", {});
    expect(outbox.enqueueLocalMutation).toHaveBeenCalledTimes(1);
  });

  it("an undone answer followed by an unticked one makes no ruling", () => {
    watchStandingAnswers();
    intend("inbox:ask-91", true, ask, "Merge");
    expect(standingAnswerIntent("inbox:ask-91")).not.toBeNull();
    // Undo, then answer again without the box.
    intend("inbox:ask-91", false, ask, "Hold a week");
    expect(standingAnswerIntent("inbox:ask-91")).toBeNull();
    queue.listeners[0]("inbox:ask-91", {});
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
  });

  it("never rules on a decision that has its own answer path", () => {
    intend("inbox:x", true, { ...ask, kind: "access" }, "Approve");
    expect(standingAnswerIntent("inbox:x")).toBeNull();
  });
});

describe("outbox senders", () => {
  it("creates and revokes, and leaves other operations alone", async () => {
    const client = {
      rulingsCreate: vi.fn(async () => ({ ruling: { id: "r1" } })),
      rulingsRevoke: vi.fn(async () => ({ ruling: { id: "r1" } })),
    };
    const body = { ask_ref: "event:a", kind: "answer", answer: "Yes" };
    expect(
      await sendStandingMutation(client, {
        operation: RULING_CREATE,
        request: { body },
      }),
    ).toEqual({ ruling: { id: "r1" } });
    expect(client.rulingsCreate).toHaveBeenCalledWith(body);
    await sendStandingMutation(client, {
      operation: RULING_REVOKE,
      request: { id: "r1" },
    });
    expect(client.rulingsRevoke).toHaveBeenCalledWith("r1");
    expect(
      await sendStandingMutation(client, { operation: "curation.ungroup" }),
    ).toBeUndefined();
  });

  it("treats an incomplete receipt as a failure to retry", async () => {
    await expect(
      sendStandingMutation(
        { rulingsCreate: async () => ({}) },
        { operation: RULING_CREATE, request: { body: {} } },
      ),
    ).rejects.toThrow("Ruling receipt is incomplete.");
  });
});
