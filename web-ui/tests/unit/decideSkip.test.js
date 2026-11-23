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
vi.mock("$lib/localWorkScope.js", () => ({
  ...scope,
  canUseLocalWorkScope: (value) => Boolean(value),
}));
const store = vi.hoisted(() => {
  const records = new Map();
  return {
    records,
    saveLocalWork: vi.fn((scope, kind, target, value) => {
      const key = `${scope}|${kind}|${target}`;
      if (value === null) records.delete(key);
      else records.set(key, { scope, kind, target, value });
      return Promise.resolve();
    }),
    listLocalWork: vi.fn(async (scope, kind) =>
      [...records.values()].filter((r) => r.scope === scope && r.kind === kind),
    ),
  };
});
vi.mock("$lib/localWorkStore.js", () => store);
const brief = vi.hoisted(() => ({ refreshBrief: vi.fn(async () => {}) }));
vi.mock("$lib/briefStore.js", () => brief);

import {
  SKIP,
  SKIP_MANY,
  SKIP_REASON,
  SKIP_UNDO_MS,
  canSkip,
  PENDING_KIND,
  flushSkip,
  pendingSkip,
  recoverPendingSkips,
  resetDecideSkip,
  skipDecisions,
  skipTarget,
  skippingItemIds,
  stuckSkipCount,
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
  store.records.clear();
  store.saveLocalWork.mockClear();
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
  it("waits behind Undo, then saves the selection as one entry", async () => {
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
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      operation: SKIP_MANY,
      title: "Skip 2 decisions",
    });
    const items = entries[0].request.items;
    expect(items).toHaveLength(2);
    expect(items[0]).toMatchObject({
      item_id: "inbox:a",
      body: {
        outcome: "skipped",
        response_text: SKIP_REASON,
        notify_mode: "original",
      },
    });
    // Only an agent requester is woken; a human reads it from their asks.
    expect(items[1].body.notify_mode).toBe("none");
    // Every send has its own keys, and the quiet fallback never reuses one.
    const keys = items.flatMap((item) => [
      item.body.idempotency_key,
      item.quiet_key,
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

  it("journals the frozen requests before the window, and Undo deletes them", () => {
    skipDecisions([agentAsk]);
    const [record] = [...store.records.values()];
    expect(record).toMatchObject({ scope: "scope-a", kind: PENDING_KIND });
    expect(record.value.entries[0].request.items[0].body.outcome).toBe(
      "skipped",
    );
    undoSkip();
    expect(store.records.size).toBe(0);
  });

  it("a crash inside the window is recovered with the frozen keys", async () => {
    skipDecisions([agentAsk]);
    const [record] = [...store.records.values()];
    // The tab dies: in-memory state is gone, the journal is not.
    resetDecideSkip();
    await vi.advanceTimersByTimeAsync(SKIP_UNDO_MS);
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
    await recoverPendingSkips();
    const sent = outbox.enqueueLocalMutation.mock.calls.map(([e]) => e);
    expect(sent).toEqual(record.value.entries);
    expect(store.records.size).toBe(0);
  });

  it("leaves another window's skip alone until its deadline", async () => {
    skipDecisions([agentAsk]);
    const [record] = [...store.records.values()];
    resetDecideSkip();
    store.records.clear();
    store.records.set("other", record);
    await recoverPendingSkips();
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(SKIP_UNDO_MS + 100);
    expect(outbox.enqueueLocalMutation).toHaveBeenCalledTimes(1);
  });

  it("keeps a skip journaled when the reader switched workspace inside the window", async () => {
    skipDecisions([agentAsk]);
    scope.localWorkScope.set("scope-b");
    await flushSkip();
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
    expect(store.records.size).toBe(1);
    scope.localWorkScope.set("scope-a");
    await vi.advanceTimersByTimeAsync(SKIP_UNDO_MS);
    await recoverPendingSkips();
    expect(outbox.enqueueLocalMutation).toHaveBeenCalledTimes(1);
  });

  it("hides rows while waiting, sending or confirmed, but not when stuck", () => {
    const many = (target, state, ids, done = {}) => ({
      operation: SKIP_MANY,
      target,
      state,
      request: { items: ids.map((item_id) => ({ item_id })) },
      receipt: { done },
    });
    const outboxRows = [
      { operation: SKIP, target: skipTarget("inbox:c"), state: "pending" },
      { operation: SKIP, target: skipTarget("inbox:d"), state: "attention" },
      { operation: "asks.not_a_decision", target: "x", state: "pending" },
      many("skip-many:1", "sending", ["inbox:g", "inbox:h"]),
      // Stuck after sending one of two: only the unsent row comes back.
      many("skip-many:2", "attention", ["inbox:i", "inbox:j"], {
        "inbox:i": "responded",
      }),
    ];
    const ids = skippingItemIds({ ids: ["inbox:a"] }, outboxRows, {
      [skipTarget("inbox:e")]: {},
      "not-a-decision:inbox:f": {},
      "skip-many:3": many("skip-many:3", "sent", ["inbox:k"]),
    });
    expect(ids.sort()).toEqual([
      "inbox:a",
      "inbox:c",
      "inbox:e",
      "inbox:g",
      "inbox:h",
      "inbox:i",
      "inbox:k",
    ]);
    const stuck = stuckSkips(outboxRows);
    expect(stuck.map((e) => e.target)).toEqual([
      skipTarget("inbox:d"),
      "skip-many:2",
    ]);
    expect(stuckSkipCount(stuck)).toBe(2);
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

  it("treats a grouped batch that was already answered as done", async () => {
    const client = {
      respondInboxItem: vi.fn(async () => {
        throw conflict("conflict", "this ask batch has already been answered");
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

describe("sending many skips", () => {
  const item = (n) => ({
    item_id: `inbox:${n}`,
    body: {
      response_text: SKIP_REASON,
      outcome: "skipped",
      notify_mode: "original",
      idempotency_key: `k${n}`,
    },
    quiet_key: `q${n}`,
  });
  const entry = (count, receipt) => ({
    operation: SKIP_MANY,
    request: { items: Array.from({ length: count }, (_, n) => item(n)) },
    receipt,
  });
  const ok = (items) => ({
    results: items.map((_, index) => ({ index, status: "responded" })),
  });

  it("sends chunks of the batch route with each item's keys", async () => {
    const client = { respondInboxItems: vi.fn(async (items) => ok(items)) };
    const checkpoint = vi.fn(async () => {});
    const result = await sendStandingMutation(client, entry(30), checkpoint);
    expect(result.skipped).toBe(30);
    expect(client.respondInboxItems).toHaveBeenCalledTimes(2);
    const [first] = client.respondInboxItems.mock.calls[0];
    expect(first).toHaveLength(25);
    expect(first[0]).toMatchObject({
      inbox_item_id: "inbox:0",
      idempotency_key: "k0",
      quiet_idempotency_key: "q0",
      outcome: "skipped",
    });
    expect(checkpoint).toHaveBeenCalledTimes(2);
  });

  it("resends only deferred items, and resumes after a checkpoint", async () => {
    const client = {
      respondInboxItems: vi
        .fn()
        .mockImplementationOnce(async () => ({
          results: [
            { index: 0, status: "responded" },
            { index: 1, status: "deferred" },
          ],
        }))
        .mockImplementation(async (items) => ok(items)),
    };
    await sendStandingMutation(
      client,
      entry(3, { done: { "inbox:0": "responded" } }),
    );
    const sent = client.respondInboxItems.mock.calls.map(([items]) =>
      items.map((i) => i.inbox_item_id),
    );
    expect(sent).toEqual([["inbox:1", "inbox:2"], ["inbox:2"]]);
  });

  it("counts asks closed elsewhere as done", async () => {
    const client = {
      respondInboxItems: vi.fn(async () => ({
        results: [{ index: 0, status: "closed", http_status: 409 }],
      })),
    };
    await expect(sendStandingMutation(client, entry(1))).resolves.toMatchObject(
      { skipped: 1 },
    );
  });

  it("names the refused items and keeps the rest done", async () => {
    const client = {
      respondInboxItems: vi.fn(async () => ({
        results: [
          { index: 0, status: "responded" },
          {
            index: 1,
            status: "failed",
            http_status: 400,
            error: { message: "inbox item is missing source event id" },
          },
        ],
      })),
    };
    const checkpoint = vi.fn(async () => {});
    const error = await sendStandingMutation(
      client,
      entry(2),
      checkpoint,
    ).catch((e) => e);
    expect(error.status).toBe(400);
    expect(error.publicMessage).toContain("1 skip could not be sent");
    expect(error.publicMessage).toContain("missing source event id");
    expect(checkpoint).toHaveBeenCalledWith({
      done: { "inbox:0": "responded" },
    });
  });

  it("retries with backoff when nothing was attempted", async () => {
    const client = {
      respondInboxItems: vi.fn(async () => ({
        results: [{ index: 0, status: "deferred" }],
      })),
    };
    const error = await sendStandingMutation(client, entry(1)).catch((e) => e);
    expect(error.status).toBe(503);
  });

  it("falls back to one request per skip on a core without the route", async () => {
    const client = {
      respondInboxItems: vi.fn(async () => {
        throw Object.assign(new Error("endpoint not found"), {
          status: 404,
          body: { error: { message: "endpoint not found" } },
        });
      }),
      respondInboxItem: vi.fn(async () => ({ event: { id: "ev" } })),
    };
    await expect(sendStandingMutation(client, entry(2))).resolves.toMatchObject(
      { skipped: 2 },
    );
    expect(client.respondInboxItem).toHaveBeenCalledTimes(2);
  });

  it("falls back too behind a proxy that has not classified the route", async () => {
    const client = {
      respondInboxItems: vi.fn(async () => {
        throw Object.assign(new Error("route not available"), {
          status: 404,
          body: { error: { code: "route_not_available" } },
        });
      }),
      respondInboxItem: vi.fn(async () => ({ event: { id: "ev" } })),
    };
    await expect(sendStandingMutation(client, entry(1))).resolves.toMatchObject(
      { skipped: 1 },
    );
  });
});
