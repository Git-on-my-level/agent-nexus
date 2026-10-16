// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  clearPrincipalLocalWork,
  createMutationSendLocker,
  listLocalWork,
  loadLocalWork,
  localWorkEpoch,
  requestMutationSendLock,
  saveLocalWork,
} from "../../src/lib/localWorkStore.js";
import { mutationFailure } from "../../src/lib/mutationStatus.js";

const scope = JSON.stringify(["org", "workspace", "human", "actor"]);
beforeEach(async () => {
  await listLocalWork(scope);
  localStorage.clear();
});
afterEach(async () => {
  vi.restoreAllMocks();
  await clearPrincipalLocalWork(scope);
});

it("checkpoints before yielding, even when IndexedDB is unavailable", async () => {
  const saved = saveLocalWork(
    scope,
    "draft",
    "inbox:one:reply",
    "Keep this text",
  );
  expect(
    [...Array(localStorage.length)].some((_, i) =>
      localStorage.getItem(localStorage.key(i)).includes("Keep this text"),
    ),
  ).toBe(true);
  await saved;
  expect(await loadLocalWork(scope, "draft", "inbox:one:reply")).toBe(
    "Keep this text",
  );
});

it("observing a remote epoch before its purge still clears visible private work", async () => {
  const principal = JSON.stringify(["human", "actor"]);
  const key = `anx:local-work:v1:principal:${principal}`;
  const listener = vi.fn();
  window.addEventListener("anx:local-work-purged", listener);
  localStorage.setItem(key, "remote-revocation");
  expect(localWorkEpoch(scope)).toBe("remote-revocation");
  window.dispatchEvent(
    new StorageEvent("storage", { key, newValue: "older-revocation" }),
  );
  expect(listener).not.toHaveBeenCalled();
  window.dispatchEvent(
    new StorageEvent("storage", { key, newValue: "remote-revocation" }),
  );
  // The single-fork suite can retain listeners from earlier module instances.
  // Every live instance must apply the first notification, and none repeats it.
  expect(listener).toHaveBeenCalled();
  const applied = listener.mock.calls.length;
  window.dispatchEvent(
    new StorageEvent("storage", { key, newValue: "remote-revocation" }),
  );
  expect(listener).toHaveBeenCalledTimes(applied);
  window.removeEventListener("anx:local-work-purged", listener);
});

it("namespaces work by organization, workspace, principal and target", async () => {
  await saveLocalWork(scope, "draft", "inbox:one:reply", "Private answer");
  for (const other of [
    ["other-org", "workspace", "human", "actor"],
    ["org", "other-workspace", "human", "actor"],
    ["org", "workspace", "other-human", "other-actor"],
  ])
    expect(
      await loadLocalWork(JSON.stringify(other), "draft", "inbox:one:reply"),
    ).toBeNull();
  expect(await loadLocalWork(scope, "draft", "inbox:two:reply")).toBeNull();
});

it("keeps independent records and does not resurrect a confirmed deletion", async () => {
  await Promise.all([
    saveLocalWork(scope, "inbox-send", "key-one", { text: "First" }),
    saveLocalWork(scope, "inbox-send", "key-two", { text: "Second" }),
  ]);
  await saveLocalWork(scope, "inbox-send", "key-one", null);
  expect(
    (await listLocalWork(scope, "inbox-send")).map((record) => record.target),
  ).toEqual(["key-two"]);
});

it("purges the principal across workspaces and fences late saves", async () => {
  const otherWorkspace = JSON.stringify(["org", "other", "human", "actor"]);
  const otherPrincipal = JSON.stringify(["org", "workspace", "other", "other"]);
  const epoch = localWorkEpoch(scope);
  sessionStorage.setItem("anx.pm.draft:workspace:new", "Legacy PM draft");
  localStorage.setItem(
    "anx.human-response.draft:workspace:item",
    "Legacy private text",
  );
  const earlierSave = saveLocalWork(scope, "draft", "reply", "Secret");
  await saveLocalWork(otherWorkspace, "draft", "reply", "Also secret");
  await saveLocalWork(
    otherPrincipal,
    "draft",
    "reply",
    "Another person's draft",
  );
  await clearPrincipalLocalWork(scope);
  expect(sessionStorage.getItem("anx.pm.draft:workspace:new")).toBeNull();
  expect(
    localStorage.getItem("anx.human-response.draft:workspace:item"),
  ).toBeNull();
  await earlierSave.catch(() => {});
  expect(await listLocalWork(scope)).toEqual([]);
  expect(await listLocalWork(otherWorkspace)).toEqual([]);
  expect(await loadLocalWork(otherPrincipal, "draft", "reply")).toBe(
    "Another person's draft",
  );
  expect(() =>
    saveLocalWork(scope, "draft", "reply", "Late secret", { epoch }),
  ).toThrow("signed out");
});

it("reports total storage failure instead of claiming work was saved", async () => {
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("quota");
  });
  await expect(
    saveLocalWork(scope, "draft", "reply", "Unsaved text"),
  ).rejects.toMatchObject({ localPersistence: true });
  expect(await loadLocalWork(scope, "draft", "reply")).toBe("Unsaved text");
  expect((await listLocalWork(scope, "draft"))[0].value).toBe("Unsaved text");
  const leaving = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(leaving);
  expect(leaving.defaultPrevented).toBe(true);
});

it("rejects anonymous namespaces and recovers other records despite corruption", async () => {
  expect(() =>
    saveLocalWork('["org","workspace","",""]', "draft", "reply", "text"),
  ).toThrow("identity");
  localStorage.setItem("anx:local-work:v1:corrupt", "broken JSON");
  await saveLocalWork(scope, "draft", "reply", "Recoverable");
  expect(await loadLocalWork(scope, "draft", "reply")).toBe("Recoverable");
});

it.each([0, 408, 429, 500, 524])(
  "retries uncertain status %s without exposing upstream bodies",
  (status) => {
    const error = Object.assign(new Error("<html>Cloudflare secret</html>"), {
      status,
    });
    const failure = mutationFailure(error);
    expect(failure.state).toBe("retrying");
    expect(failure.message).not.toContain("Cloudflare");
    expect(failure.detail).toContain("Cloudflare");
  },
);
it.each([400, 401, 403, 404, 409, 422])(
  "retains definitive status %s for attention",
  (status) => {
    expect(mutationFailure({ status, message: "server detail" })).toMatchObject(
      { state: "attention" },
    );
  },
);

it("preserves new-session work when the same cross-tab purge is delivered again", async () => {
  await saveLocalWork(scope, "draft", "reply", "Before sign-out");
  const epoch = crypto.randomUUID();
  await clearPrincipalLocalWork(scope, { epoch });
  await saveLocalWork(scope, "draft", "reply", "After sign-in");
  await clearPrincipalLocalWork(scope, { epoch, broadcast: false });
  expect(await loadLocalWork(scope, "draft", "reply")).toBe("After sign-in");
});

it("serializes sends across tabs via a localStorage lease when navigator.locks is unavailable", async () => {
  const prevLocks = navigator.locks;
  Object.defineProperty(navigator, "locks", {
    configurable: true,
    value: undefined,
  });
  try {
    const leaseKey = `anx:mutation-lock:anx:mutation-send:${scope}`;
    localStorage.setItem(
      leaseKey,
      JSON.stringify({
        owner: "other-tab",
        token: "other-token",
        expiresAt: Date.now() + 5_000,
      }),
    );
    let entered = false;
    const locked = requestMutationSendLock(scope, async () => {
      entered = true;
      return "ok";
    });
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(entered).toBe(false);
    localStorage.removeItem(leaseKey);
    window.dispatchEvent(
      new StorageEvent("storage", { key: leaseKey, newValue: null }),
    );
    expect(await locked).toBe("ok");
    expect(entered).toBe(true);
    expect(localStorage.getItem(leaseKey)).toBeNull();
  } finally {
    Object.defineProperty(navigator, "locks", {
      configurable: true,
      value: prevLocks,
    });
  }
});

function createSharedTabEnvironment({ dropFirstCollision = false } = {}) {
  const store = new Map();
  const peers = new Set();
  let firstReads = 0;
  function makeStorage() {
    return {
      getItem(key) {
        if (dropFirstCollision && firstReads < 2) {
          firstReads += 1;
          return null;
        }
        return store.has(key) ? store.get(key) : null;
      },
      setItem(key, value) {
        store.set(key, String(value));
      },
      removeItem(key) {
        store.delete(key);
      },
    };
  }
  function makeChannel() {
    const channel = {
      onmessage: null,
      postMessage(data) {
        for (const peer of peers) {
          if (peer !== channel && typeof peer.onmessage === "function") {
            peer.onmessage({ data });
          }
        }
      },
    };
    peers.add(channel);
    return channel;
  }
  return { store, makeStorage, makeChannel };
}

it("prevents concurrent entry on simultaneous acquisition and renews the lease across a long callback", async () => {
  const env = createSharedTabEnvironment({ dropFirstCollision: true });
  const tabA = createMutationSendLocker({
    tabId: "tab-a",
    storage: env.makeStorage(),
    channel: env.makeChannel(),
    locks: null,
    leaseTtlMs: 60,
    heartbeatIntervalMs: 15,
    pollMs: 10,
    settleMinMs: 5,
    settleMaxMs: 12,
  });
  const tabB = createMutationSendLocker({
    tabId: "tab-b",
    storage: env.makeStorage(),
    channel: env.makeChannel(),
    locks: null,
    leaseTtlMs: 60,
    heartbeatIntervalMs: 15,
    pollMs: 10,
    settleMinMs: 5,
    settleMaxMs: 12,
  });

  let activeCount = 0;
  let maxConcurrent = 0;
  const events = [];

  const runTab = (label, locker, holdMs) =>
    locker.requestMutationSendLock(scope, async (lease) => {
      activeCount += 1;
      maxConcurrent = Math.max(maxConcurrent, activeCount);
      events.push(`enter:${label}`);
      await new Promise((resolve) => setTimeout(resolve, holdMs));
      expect(lease.isHeld()).toBe(true);
      events.push(`exit:${label}`);
      activeCount -= 1;
      return label;
    });

  // Hold the first callback for 160ms (> 2.5x the 60ms TTL) so heartbeat renewal is required.
  const [first, second] = await Promise.all([
    runTab("A", tabA, 160),
    runTab("B", tabB, 20),
  ]);
  expect([first, second].sort()).toEqual(["A", "B"]);
  expect(maxConcurrent).toBe(1);
  expect(events).toHaveLength(4);
  expect(events[0].startsWith("enter:")).toBe(true);
  expect(events[1]).toBe(`exit:${events[0].slice(6)}`);
});

it("fences checkpoint writes when a tab loses its lease during a stalled callback", async () => {
  const env = createSharedTabEnvironment();
  let stallHeartbeatA = false;
  const storageA = {
    getItem: (key) => env.makeStorage().getItem(key),
    setItem: (key, value) => {
      if (stallHeartbeatA) return;
      env.makeStorage().setItem(key, value);
    },
    removeItem: (key) => env.makeStorage().removeItem(key),
  };
  const tabA = createMutationSendLocker({
    tabId: "tab-a",
    storage: storageA,
    channel: env.makeChannel(),
    locks: null,
    leaseTtlMs: 50,
    heartbeatIntervalMs: 15,
    pollMs: 10,
    settleMinMs: 2,
    settleMaxMs: 5,
  });
  const tabB = createMutationSendLocker({
    tabId: "tab-b",
    storage: env.makeStorage(),
    channel: env.makeChannel(),
    locks: null,
    leaseTtlMs: 50,
    heartbeatIntervalMs: 15,
    pollMs: 10,
    settleMinMs: 2,
    settleMaxMs: 5,
  });

  let releaseStalledA;
  const stalledGateA = new Promise((resolve) => {
    releaseStalledA = resolve;
  });
  let enteredA;
  const enteredGateA = new Promise((resolve) => {
    enteredA = resolve;
  });

  const promiseA = tabA.requestMutationSendLock(scope, async (lease) => {
    await saveLocalWork(
      scope,
      "mutation",
      "m-fenced",
      { state: "sending", by: "A" },
      { lease },
    );
    stallHeartbeatA = true;
    enteredA();
    await stalledGateA;
    // Lease was stolen by tab B while A was stalled; checkpoint must be fenced.
    await saveLocalWork(
      scope,
      "mutation",
      "m-fenced",
      { state: "sending", by: "A-stale" },
      { lease },
    );
  });

  await enteredGateA;
  // Wait for A's unrenewed 50ms lease to expire, then let B acquire and checkpoint.
  await new Promise((resolve) => setTimeout(resolve, 70));
  await tabB.requestMutationSendLock(scope, async (lease) => {
    await saveLocalWork(
      scope,
      "mutation",
      "m-fenced",
      { state: "confirmed", by: "B" },
      { lease },
    );
  });

  releaseStalledA();
  await expect(promiseA).rejects.toMatchObject({ leaseLost: true });
  expect(await loadLocalWork(scope, "mutation", "m-fenced")).toEqual({
    state: "confirmed",
    by: "B",
  });
});

it("logs a warning and falls back to in-tab exclusion when lock storage throws", async () => {
  const warn = vi.fn();
  const brokenStorage = {
    getItem() {
      throw new Error("storage disabled");
    },
    setItem() {
      throw new Error("storage disabled");
    },
    removeItem() {},
  };
  const locker = createMutationSendLocker({
    tabId: "tab-broken",
    storage: brokenStorage,
    channel: null,
    locks: null,
    warn,
  });
  const order = [];
  await Promise.all([
    locker.requestMutationSendLock(scope, async () => {
      order.push("start:1");
      await new Promise((resolve) => setTimeout(resolve, 15));
      order.push("end:1");
    }),
    locker.requestMutationSendLock(scope, async () => {
      order.push("start:2");
      order.push("end:2");
    }),
  ]);
  expect(order).toEqual(["start:1", "end:1", "start:2", "end:2"]);
  expect(warn).toHaveBeenCalled();
});
