// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * A row an older tab marked "Unknown saved operation" is revived and sent in
 * one hold of the send lock. Nothing is written as "pending" outside the
 * lock, so an older tab queued on the lock finds the row already confirmed
 * instead of failing it again.
 */
const SCOPE = JSON.stringify(["org", "ws", "", "actor-1"]);

const h = vi.hoisted(() => ({
  records: new Map(),
  writes: [],
  lockHeld: false,
  respondInboxItem: null,
}));

vi.mock("$app/navigation", () => ({ goto: vi.fn() }));
vi.mock("$lib/coreClient.js", () => ({
  createLocalMutationClient: () => ({
    respondInboxItem: (...args) => h.respondInboxItem(...args),
  }),
}));
vi.mock("$lib/localWorkScope.js", async () => {
  const { writable } = await import("svelte/store");
  return {
    localWorkScope: writable(SCOPE),
    canUseLocalWorkScope: () => true,
  };
});
vi.mock("$lib/authSession.js", async () => {
  const { writable } = await import("svelte/store");
  return { authenticatedAgent: writable({ actor_id: "actor-1" }) };
});
vi.mock("$lib/workspaceContext.js", async () => {
  const { writable } = await import("svelte/store");
  return { devActorMode: writable(false) };
});
vi.mock("$lib/localWorkStore.js", () => {
  const key = (kind, target) => `${kind}|${target}`;
  return {
    localWorkEpoch: () => "initial",
    localWorkPrincipal: () => "principal",
    onLocalWorkChanged: () => () => {},
    protectLocalWorkPreparation: () => () => {},
    requestMutationSendLock: async (_scope, callback) => {
      h.lockHeld = true;
      try {
        return await callback({ isHeld: () => true, assertHeld: () => {} });
      } finally {
        h.lockHeld = false;
      }
    },
    saveLocalWork: async (_scope, kind, target, value, options = {}) => {
      h.writes.push({ kind, target, state: value?.state, held: h.lockHeld });
      if (value === null)
        h.records.set(key(kind, target), {
          target,
          deleted: true,
          deletionReason: options.deletionReason,
          confirmation: options.confirmation,
          submission: options.submission,
        });
      else
        h.records.set(key(kind, target), {
          target,
          value: JSON.parse(JSON.stringify(value)),
        });
    },
    loadLocalWorkRecord: async (_scope, kind, target) =>
      h.records.get(key(kind, target)) ?? null,
    loadLocalWork: async (_scope, kind, target) =>
      h.records.get(key(kind, target))?.value ?? null,
    listLocalWork: async (_scope, kind, { includeDeleted = false } = {}) =>
      [...h.records.entries()]
        .filter(([k]) => k.startsWith(`${kind}|`))
        .map(([, record]) => record)
        .filter((record) => includeDeleted || !record.deleted),
  };
});

const { recoverMutations } = await import("../../src/lib/mutationOutbox.js");

function stuckSkip(key, operation = "inbox.skip") {
  h.records.set(`mutation|${key}`, {
    target: key,
    value: {
      key,
      scope: SCOPE,
      epoch: "initial",
      operation,
      target: "skip:inbox:a",
      request: {
        item_id: "inbox:a",
        body: { response_text: "Skipped", outcome: "skipped" },
      },
      state: "attention",
      failure: { state: "attention", detail: "Unknown saved operation" },
      attempts: 1,
      nextAt: 0,
      createdAt: Date.now(),
    },
  });
}

beforeEach(() => {
  h.records.clear();
  h.writes.length = 0;
  h.respondInboxItem = vi.fn(async () => ({ event: { id: "ev" } }));
});

describe("reviving rows an older tab gave up on", () => {
  it("sends the row in the same lock hold, never writing it pending outside", async () => {
    stuckSkip("k1");
    await recoverMutations();
    expect(h.respondInboxItem).toHaveBeenCalledTimes(1);
    const mutationWrites = h.writes.filter((w) => w.kind === "mutation");
    expect(mutationWrites.length).toBeGreaterThan(0);
    expect(mutationWrites.every((w) => w.held)).toBe(true);
    expect(mutationWrites.some((w) => w.state === "pending")).toBe(false);
    expect(h.records.get("mutation|k1")).toMatchObject({
      deleted: true,
      deletionReason: "confirmed",
    });
  });

  it("revives a row once per tab: a second failure waits for the reader", async () => {
    stuckSkip("k2");
    h.respondInboxItem = vi.fn(async () => {
      throw Object.assign(new Error("Unknown saved operation"), {
        status: 400,
      });
    });
    await recoverMutations();
    await recoverMutations();
    expect(h.respondInboxItem).toHaveBeenCalledTimes(1);
    expect(h.records.get("mutation|k2").value.state).toBe("attention");
  });

  it("leaves an operation this build does not know untouched", async () => {
    stuckSkip("k3", "from.a.newer.build");
    h.records.get("mutation|k3").value.state = "pending";
    await recoverMutations();
    expect(h.writes.filter((w) => w.kind === "mutation")).toEqual([]);
  });
});
