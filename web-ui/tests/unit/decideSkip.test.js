import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

const outbox = await vi.hoisted(async () => {
  const { writable } = await import("svelte/store");
  return {
    enqueueLocalMutation: vi.fn(async (entry) => entry),
    mutationConfirmations: writable({}),
  };
});
vi.mock("$lib/mutationOutbox.js", () => outbox);
const scope = await vi.hoisted(async () => {
  const { writable } = await import("svelte/store");
  return { localWorkScope: writable("scope-a") };
});
vi.mock("$lib/localWorkScope.js", () => scope);
const brief = vi.hoisted(() => ({ refreshBrief: vi.fn(async () => {}) }));
vi.mock("$lib/briefStore.js", () => brief);

import {
  SKIP,
  SKIP_REASON,
  SKIP_UNDO_MS,
  canSkip,
  flushSkip,
  pendingSkip,
  resetDecideSkip,
  skipDecisions,
  skipTarget,
  skippingItemIds,
  stuckSkips,
  undoSkip,
} from "../../src/lib/decideSkip.js";
import { sendStandingMutation } from "../../src/lib/standingRulingSend.js";

const agentAsk = {
  kind: "ask",
  inboxItemId: "inbox:a",
  title: "Merge?",
  requestedBy: { kind: "agent", display: "Release" },
};
const humanReview = {
  kind: "review",
  inboxItemId: "inbox:b",
  title: "Review copy",
  requestedBy: { kind: "human", display: "Omar" },
};

beforeEach(() => {
  vi.useFakeTimers();
  resetDecideSkip();
  scope.localWorkScope.set("scope-a");
  outbox.enqueueLocalMutation.mockClear();
  outbox.mutationConfirmations.set({});
  brief.refreshBrief.mockClear();
});
afterEach(() => vi.useRealTimers());

describe("which decisions can be skipped", () => {
  it("is anything answered through the inbox respond route", () => {
    expect(canSkip(agentAsk)).toBe(true);
    expect(canSkip(humanReview)).toBe(true);
    expect(canSkip({ ...agentAsk, batch: { count: 3 } })).toBe(true);
    expect(canSkip({ ...agentAsk, kind: "pm_proposal" })).toBe(true);
    for (const other of [
      { ...agentAsk, inboxItemId: "" },
      { ...agentAsk, kind: "access" },
      { ...agentAsk, kind: "host" },
      { ...agentAsk, kind: "pm_proposal", pmDecision: { id: "d1" } },
      { ...agentAsk, accessRequestId: "ar1" },
    ])
      expect(canSkip(other)).toBe(false);
  });
});

describe("skipping", () => {
  it("waits behind Undo, then saves one skipped response per decision", async () => {
    const count = skipDecisions(
      [agentAsk, humanReview, { ...agentAsk, kind: "access" }],
      { href: "/decide" },
    );
    expect(count).toBe(2);
    expect(get(pendingSkip).ids).toEqual(["inbox:a", "inbox:b"]);
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(SKIP_UNDO_MS);
    expect(get(pendingSkip)).toBeNull();
    const entries = outbox.enqueueLocalMutation.mock.calls.map(([e]) => e);
    expect(entries).toHaveLength(2);
    expect(entries[0]).toMatchObject({
      operation: SKIP,
      target: skipTarget("inbox:a"),
      request: {
        item_id: "inbox:a",
        body: {
          outcome: "skipped",
          response_text: SKIP_REASON,
          notify_mode: "original",
        },
      },
    });
    // Only an agent requester is woken; a human reads it from their asks.
    expect(entries[1].request.body.notify_mode).toBe("none");
    // Every send has its own keys, and the quiet fallback never reuses one.
    const keys = entries.flatMap((e) => [
      e.request.body.idempotency_key,
      e.request.quiet_key,
    ]);
    expect(new Set(keys).size).toBe(4);
  });

  it("never says the decision was made or ruled", () => {
    expect(SKIP_REASON).toMatch(/without a decision/i);
    expect(SKIP_REASON).toMatch(/ask again later/i);
  });

  it("Undo inside the window sends nothing", async () => {
    skipDecisions([agentAsk]);
    expect(undoSkip()).toEqual(["inbox:a"]);
    await vi.advanceTimersByTimeAsync(SKIP_UNDO_MS * 2);
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
    expect(undoSkip()).toBeNull();
  });

  it("a newer skip saves the earlier one at once; only the newest is undoable", async () => {
    skipDecisions([agentAsk]);
    skipDecisions([humanReview]);
    await Promise.resolve();
    expect(outbox.enqueueLocalMutation).toHaveBeenCalledTimes(1);
    expect(undoSkip()).toEqual(["inbox:b"]);
  });

  it("drops a skip when the reader switched workspace inside the window", async () => {
    skipDecisions([agentAsk]);
    scope.localWorkScope.set("scope-b");
    await flushSkip();
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
  });

  it("hides rows while waiting, sending or confirmed, but not when stuck", () => {
    const outboxRows = [
      { operation: SKIP, target: skipTarget("inbox:c"), state: "pending" },
      { operation: SKIP, target: skipTarget("inbox:d"), state: "attention" },
      { operation: "asks.not_a_decision", target: "x", state: "pending" },
    ];
    const ids = skippingItemIds({ ids: ["inbox:a"] }, outboxRows, {
      [skipTarget("inbox:e")]: {},
      "not-a-decision:inbox:f": {},
    });
    expect(ids.sort()).toEqual(["inbox:a", "inbox:c", "inbox:e"]);
    expect(stuckSkips(outboxRows).map((e) => e.target)).toEqual([
      skipTarget("inbox:d"),
    ]);
  });

  it("reads the brief once after a burst of confirmations", async () => {
    skipDecisions([agentAsk, humanReview]);
    await flushSkip();
    outbox.mutationConfirmations.set({ [skipTarget("inbox:a")]: {} });
    outbox.mutationConfirmations.set({
      [skipTarget("inbox:a")]: {},
      [skipTarget("inbox:b")]: {},
    });
    await vi.advanceTimersByTimeAsync(1_000);
    expect(brief.refreshBrief).toHaveBeenCalledTimes(1);
  });
});

describe("sending a skip", () => {
  const request = {
    item_id: "inbox:a",
    body: {
      response_text: SKIP_REASON,
      outcome: "skipped",
      notify_mode: "original",
      idempotency_key: "k1",
    },
    quiet_key: "k2",
  };
  const conflict = (code, message = "") =>
    Object.assign(new Error(message), {
      status: 409,
      body: { error: { code, message } },
    });

  it("responds once with the saved body", async () => {
    const client = {
      respondInboxItem: vi.fn(async () => ({ event: { id: "ev" } })),
    };
    await expect(
      sendStandingMutation(client, { operation: SKIP, request }),
    ).resolves.toEqual({ skipped: true });
    expect(client.respondInboxItem).toHaveBeenCalledWith(
      "inbox:a",
      request.body,
    );
  });

  it("accepts a grouped batch receipt", async () => {
    const client = {
      respondInboxItem: vi.fn(async () => ({ batch_id: "b1", answers: [] })),
    };
    await expect(
      sendStandingMutation(client, { operation: SKIP, request }),
    ).resolves.toEqual({ skipped: true });
  });

  it("treats an ask that closed elsewhere as done", async () => {
    const client = {
      respondInboxItem: vi.fn(async () => {
        throw conflict("conflict", "Ask is no longer open");
      }),
    };
    await expect(
      sendStandingMutation(client, { operation: SKIP, request }),
    ).resolves.toEqual({ skipped: false, closed: true });
  });

  it("records without a wake, under its own key, when the requester is unreachable", async () => {
    const client = {
      respondInboxItem: vi
        .fn()
        .mockRejectedValueOnce(conflict("notification_target_required"))
        .mockResolvedValueOnce({ event: { id: "ev" } }),
    };
    await sendStandingMutation(client, { operation: SKIP, request });
    expect(client.respondInboxItem.mock.calls[1][1]).toEqual({
      ...request.body,
      notify_mode: "none",
      idempotency_key: "k2",
    });
  });

  it("surfaces any other failure for the outbox to retry", async () => {
    const client = {
      respondInboxItem: vi.fn(async () => {
        throw Object.assign(new Error("boom"), { status: 500 });
      }),
    };
    await expect(
      sendStandingMutation(client, { operation: SKIP, request }),
    ).rejects.toThrow("boom");
  });

  it("rejects an incomplete receipt", async () => {
    const client = { respondInboxItem: vi.fn(async () => ({})) };
    await expect(
      sendStandingMutation(client, { operation: SKIP, request }),
    ).rejects.toThrow(/incomplete/);
  });
});
