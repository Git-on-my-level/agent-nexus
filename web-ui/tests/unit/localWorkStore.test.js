// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  clearPrincipalLocalWork,
  createMutationSendLocker,
  listLocalWork,
  loadLocalWork,
  loadLocalWorkRecord,
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

function createSharedTabEnvironment() {
  const store = new Map();
  const peers = new Set();
  function makeStorage() {
    return {
      getItem(key) {
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

it("renews the best-effort cross-tab lease across a long callback", async () => {
  const env = createSharedTabEnvironment();
  const tabA = createMutationSendLocker({
    tabId: "tab-a",
    storage: env.makeStorage(),
    channel: env.makeChannel(),
    locks: null,
    leaseTtlMs: 60,
    heartbeatIntervalMs: 15,
    pollMs: 10,
  });
  const tabB = createMutationSendLocker({
    tabId: "tab-b",
    storage: env.makeStorage(),
    channel: env.makeChannel(),
    locks: null,
    leaseTtlMs: 60,
    heartbeatIntervalMs: 15,
    pollMs: 10,
  });

  let activeCount = 0;
  let maxConcurrent = 0;
  const events = [];
  let enteredA;
  const gateA = new Promise((resolve) => {
    enteredA = resolve;
  });

  const runA = tabA.requestMutationSendLock(scope, async (lease) => {
    activeCount += 1;
    maxConcurrent = Math.max(maxConcurrent, activeCount);
    events.push("enter:A");
    enteredA();
    // Hold for 160ms (> 2.5x the 60ms TTL) so heartbeat renewal is required.
    await new Promise((resolve) => setTimeout(resolve, 160));
    expect(lease.isHeld()).toBe(true);
    events.push("exit:A");
    activeCount -= 1;
    return "A";
  });
  await gateA;
  const runB = tabB.requestMutationSendLock(scope, async (lease) => {
    activeCount += 1;
    maxConcurrent = Math.max(maxConcurrent, activeCount);
    events.push("enter:B");
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(lease.isHeld()).toBe(true);
    events.push("exit:B");
    activeCount -= 1;
    return "B";
  });

  const [first, second] = await Promise.all([runA, runB]);
  expect([first, second]).toEqual(["A", "B"]);
  expect(maxConcurrent).toBe(1);
  expect(events).toEqual(["enter:A", "exit:A", "enter:B", "exit:B"]);
});

it("two tabs entering the send path for the same outbox item send the same persisted key and produce one server effect", async () => {
  const env = createSharedTabEnvironment();
  const persistedKey = "req-shared-outbox-item-1";
  await saveLocalWork(scope, "mutation", persistedKey, {
    key: persistedKey,
    scope,
    epoch: localWorkEpoch(scope),
    operation: "events.create",
    state: "pending",
    attempts: 0,
    createdAt: Date.now(),
    nextAt: Date.now(),
    request: {
      request_key: persistedKey,
      event: {
        type: "message_posted",
        thread_id: "thread-1",
        refs: ["thread:thread-1"],
        summary: "Shared outbox item",
        payload: { text: "Shared outbox item" },
      },
    },
  });

  // Simulate Tab B pausing right after its initial empty lease read, letting
  // Tab A acquire and enter its callback, then resuming Tab B so BOTH tabs
  // enter the send callback concurrently for the same persisted item.
  const rawStorageB = env.makeStorage();
  let bFirstRead = true;
  const storageB = {
    getItem(key) {
      if (bFirstRead && key.startsWith("anx:mutation-lock:")) {
        bFirstRead = false;
        return null;
      }
      return rawStorageB.getItem(key);
    },
    setItem: (key, value) => rawStorageB.setItem(key, value),
    removeItem: (key) => rawStorageB.removeItem(key),
  };

  const tabA = createMutationSendLocker({
    tabId: "tab-a",
    storage: env.makeStorage(),
    channel: env.makeChannel(),
    locks: null,
  });
  const tabB = createMutationSendLocker({
    tabId: "tab-b",
    storage: storageB,
    channel: env.makeChannel(),
    locks: null,
  });

  const serverEffects = [];
  const serverReplays = new Map();
  const seenKeys = [];
  async function mockServerCreateEvent(body) {
    seenKeys.push(body.request_key);
    if (!serverReplays.has(body.request_key)) {
      const created = {
        event: {
          ...body.event,
          id: `evt-${serverEffects.length + 1}`,
        },
      };
      serverEffects.push(created.event);
      serverReplays.set(body.request_key, created);
    }
    return serverReplays.get(body.request_key);
  }

  let enteredA;
  const gateEnteredA = new Promise((resolve) => {
    enteredA = resolve;
  });
  let bothInSend;
  const gateBothInSend = new Promise((resolve) => {
    bothInSend = resolve;
  });

  let activeSenders = 0;
  let maxConcurrentSenders = 0;

  const drainInTab = (locker, label) =>
    locker
      .requestMutationSendLock(scope, async (lease) => {
        activeSenders += 1;
        maxConcurrentSenders = Math.max(maxConcurrentSenders, activeSenders);
        if (label === "A") {
          enteredA();
          await gateBothInSend;
        } else {
          bothInSend();
        }
        const record = await loadLocalWorkRecord(
          scope,
          "mutation",
          persistedKey,
        );
        const item = record.value;
        const result = await mockServerCreateEvent(item.request);
        activeSenders -= 1;
        await saveLocalWork(scope, "mutation", persistedKey, null, {
          epoch: item.epoch,
          lease,
          deletionReason: "confirmed",
          confirmation: result,
        });
        return result;
      })
      .catch(async (error) => {
        if (!error?.leaseLost) throw error;
        const latest = await loadLocalWorkRecord(
          scope,
          "mutation",
          persistedKey,
        );
        return latest?.confirmation;
      });

  const sendA = drainInTab(tabA, "A");
  await gateEnteredA;
  const sendB = drainInTab(tabB, "B");
  const [resA, resB] = await Promise.all([sendA, sendB]);

  expect(maxConcurrentSenders).toBe(2);
  expect(seenKeys).toEqual([persistedKey, persistedKey]);
  expect(serverEffects).toHaveLength(1);
  expect(resB).toEqual({
    event: {
      type: "message_posted",
      thread_id: "thread-1",
      refs: ["thread:thread-1"],
      summary: "Shared outbox item",
      payload: { text: "Shared outbox item" },
      id: "evt-1",
    },
  });
  if (resA) expect(resA).toEqual(resB);

  // A late unconfirmed checkpoint write from a racing tab must not resurrect a confirmed item.
  await saveLocalWork(scope, "mutation", persistedKey, {
    key: persistedKey,
    state: "sending",
  });
  const finalRecord = await loadLocalWorkRecord(
    scope,
    "mutation",
    persistedKey,
  );
  expect(finalRecord?.deleted).toBe(true);
  expect(finalRecord?.deletionReason).toBe("confirmed");
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
  });
  const tabB = createMutationSendLocker({
    tabId: "tab-b",
    storage: env.makeStorage(),
    channel: env.makeChannel(),
    locks: null,
    leaseTtlMs: 50,
    heartbeatIntervalMs: 15,
    pollMs: 10,
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

it("logs a warning, falls back to in-tab exclusion, and sends at most once per tab with the persisted key when storage is unavailable", async () => {
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
  const persistedKey = "req-persisted-no-storage";
  const confirmedInTab = new Set();
  const sentKeys = [];
  const order = [];
  const attemptInTab = (id) =>
    locker.requestMutationSendLock(scope, async () => {
      order.push(`start:${id}`);
      if (!confirmedInTab.has(persistedKey)) {
        await new Promise((resolve) => setTimeout(resolve, 15));
        sentKeys.push(persistedKey);
        confirmedInTab.add(persistedKey);
      }
      order.push(`end:${id}`);
    });
  await Promise.all([attemptInTab(1), attemptInTab(2)]);
  expect(order).toEqual(["start:1", "end:1", "start:2", "end:2"]);
  expect(sentKeys).toEqual([persistedKey]);
  expect(warn).toHaveBeenCalled();
});
