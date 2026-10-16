import { randomUUID } from "./replayPolicy.js";

/** Device-only user work. IndexedDB is authoritative; a synchronous journal
 * closes the keystroke/tab-close gap and also serves as the fallback. One key
 * per record prevents unrelated tabs from overwriting each other's work. */
const PREFIX = "anx:local-work:v1:";
const DB_NAME = "anx-local-work-v1";
let database;
let operations = Promise.resolve();
let lastWrite = 0;
const listeners = new Set();
const epochs = new Map();
const appliedRevocations = new Map();
const uncheckpointed = new Map();
const preparing = new Map();
const LOCK_LEASE_TTL_MS = 30_000;
const LOCK_POLL_MS = 40;
const LOCK_SETTLE_MIN_MS = 4;
const LOCK_SETTLE_MAX_MS = 12;

export function createMutationSendLocker({
  tabId = randomUUID(),
  storage = () =>
    typeof localStorage !== "undefined" ? localStorage : undefined,
  channel: customChannel,
  locks = () => globalThis.navigator?.locks,
  now = () => Date.now(),
  leaseTtlMs = LOCK_LEASE_TTL_MS,
  heartbeatIntervalMs = Math.min(
    10_000,
    Math.max(10, Math.floor(leaseTtlMs / 3)),
  ),
  pollMs = LOCK_POLL_MS,
  settleMinMs,
  settleMaxMs,
  warn = (message, error) => console.warn(message, error),
} = {}) {
  const explicitSettle = settleMinMs !== undefined || settleMaxMs !== undefined;
  const effectiveSettleMinMs = settleMinMs ?? LOCK_SETTLE_MIN_MS;
  const effectiveSettleMaxMs = settleMaxMs ?? LOCK_SETTLE_MAX_MS;
  const inTabLocks = new Map();
  const lockWaiters = new Set();
  const resolveStorage = () =>
    typeof storage === "function" ? storage() : storage;
  const resolveLocks = () => (typeof locks === "function" ? locks() : locks);
  const resolveChannel = () =>
    customChannel !== undefined ? customChannel : channel;

  function notifyLockWaiters() {
    for (const wake of lockWaiters) wake();
  }

  const ch = resolveChannel();
  if (ch && ch !== channel) {
    const prevOnMessage = ch.onmessage;
    ch.onmessage = (event) => {
      if (event.data?.lockReleased) {
        notifyLockWaiters();
        return;
      }
      prevOnMessage?.(event);
    };
  }

  function createInTabOnlyLease() {
    let released = false;
    return {
      isHeld: () => !released,
      assertHeld: () => {
        if (released) {
          const error = new Error("Mutation send lock was released.");
          error.leaseLost = true;
          throw error;
        }
      },
      release: () => {
        released = true;
      },
    };
  }

  async function acquireCrossTabLease(name) {
    const store = resolveStorage();
    if (!store) {
      warn(
        "Cross-tab mutation lock storage is unavailable; falling back to in-tab exclusion.",
      );
      return createInTabOnlyLease();
    }
    const leaseKey = `anx:mutation-lock:${name}`;
    while (true) {
      const currentNow = now();
      let current = null;
      try {
        const raw = store.getItem(leaseKey);
        current = raw ? JSON.parse(raw) : null;
      } catch (error) {
        warn(
          "Cross-tab mutation lock storage is unavailable; falling back to in-tab exclusion.",
          error,
        );
        return createInTabOnlyLease();
      }
      if (
        !current ||
        current.owner === tabId ||
        !(current.expiresAt > currentNow)
      ) {
        const token = randomUUID();
        const candidate = {
          owner: tabId,
          token,
          expiresAt: currentNow + leaseTtlMs,
        };
        try {
          store.setItem(leaseKey, JSON.stringify(candidate));
          const immediateRaw = store.getItem(leaseKey);
          const immediate = immediateRaw ? JSON.parse(immediateRaw) : null;
          if (
            !immediate ||
            immediate.owner !== tabId ||
            immediate.token !== token
          ) {
            current = immediate;
          } else {
            const settleSpan = Math.max(
              0,
              effectiveSettleMaxMs - effectiveSettleMinMs,
            );
            const settleMs =
              effectiveSettleMinMs +
              Math.floor(Math.random() * (settleSpan + 1));
            if (
              settleMs > 0 &&
              (explicitSettle || !globalThis.setTimeout?.clock)
            ) {
              await new Promise((resolve) => setTimeout(resolve, settleMs));
            } else {
              await Promise.resolve();
            }
            const verifyRaw = store.getItem(leaseKey);
            const verify = verifyRaw ? JSON.parse(verifyRaw) : null;
            if (
              verify &&
              verify.owner === tabId &&
              verify.token === token &&
              verify.expiresAt > now()
            ) {
              let released = false;
              let lost = false;
              let heartbeatTimer = null;
              const clearHeartbeat = () => {
                if (heartbeatTimer !== null) {
                  clearInterval(heartbeatTimer);
                  heartbeatTimer = null;
                }
              };
              const renew = () => {
                if (released || lost) return false;
                try {
                  const latestRaw = store.getItem(leaseKey);
                  const latest = latestRaw ? JSON.parse(latestRaw) : null;
                  if (
                    !latest ||
                    latest.owner !== tabId ||
                    latest.token !== token
                  ) {
                    lost = true;
                    clearHeartbeat();
                    return false;
                  }
                  const refreshed = {
                    owner: tabId,
                    token,
                    expiresAt: now() + leaseTtlMs,
                  };
                  store.setItem(leaseKey, JSON.stringify(refreshed));
                  const checkRaw = store.getItem(leaseKey);
                  const check = checkRaw ? JSON.parse(checkRaw) : null;
                  if (
                    !check ||
                    check.owner !== tabId ||
                    check.token !== token
                  ) {
                    lost = true;
                    clearHeartbeat();
                    return false;
                  }
                  return true;
                } catch (error) {
                  warn(
                    "Cross-tab mutation lock renewal failed; fencing further checkpoint writes.",
                    error,
                  );
                  lost = true;
                  clearHeartbeat();
                  return false;
                }
              };
              if (heartbeatIntervalMs > 0) {
                heartbeatTimer = setInterval(renew, heartbeatIntervalMs);
              }
              const isHeld = () => {
                if (released || lost) return false;
                try {
                  const latestRaw = store.getItem(leaseKey);
                  const latest = latestRaw ? JSON.parse(latestRaw) : null;
                  if (
                    !latest ||
                    latest.owner !== tabId ||
                    latest.token !== token ||
                    !(latest.expiresAt > now())
                  ) {
                    lost = true;
                    clearHeartbeat();
                    return false;
                  }
                  return true;
                } catch {
                  lost = true;
                  clearHeartbeat();
                  return false;
                }
              };
              const assertHeld = () => {
                if (!isHeld()) {
                  const error = new Error(
                    "Cross-tab mutation send lease was lost.",
                  );
                  error.leaseLost = true;
                  throw error;
                }
              };
              const release = () => {
                released = true;
                clearHeartbeat();
                try {
                  const latestRaw = store.getItem(leaseKey);
                  const latest = latestRaw ? JSON.parse(latestRaw) : null;
                  if (
                    latest &&
                    latest.owner === tabId &&
                    latest.token === token
                  ) {
                    store.removeItem(leaseKey);
                  }
                } catch {
                  /* Best-effort cross-tab lease release. */
                }
                resolveChannel()?.postMessage({ lockReleased: name });
                notifyLockWaiters();
              };
              return { isHeld, assertHeld, release };
            }
            current = verify;
          }
        } catch (error) {
          warn(
            "Cross-tab mutation lock storage is unavailable; falling back to in-tab exclusion.",
            error,
          );
          return createInTabOnlyLease();
        }
      }
      const waitNow = now();
      const waitMs = Math.min(
        pollMs,
        Math.max(10, (current?.expiresAt ?? waitNow + pollMs) - waitNow),
      );
      await new Promise((resolve) => {
        let timer;
        const wake = () => {
          clearTimeout(timer);
          lockWaiters.delete(wake);
          resolve();
        };
        lockWaiters.add(wake);
        timer = setTimeout(wake, waitMs);
      });
    }
  }

  function requestLock(scope, callback) {
    const name = `anx:mutation-send:${scope}`;
    const locksApi = resolveLocks();
    if (locksApi?.request) {
      const webLease = createInTabOnlyLease();
      return locksApi.request(name, async () => {
        try {
          return await callback(webLease);
        } finally {
          webLease.release();
        }
      });
    }
    const previous = inTabLocks.get(name) ?? Promise.resolve();
    const run = previous
      .catch(() => {})
      .then(async () => {
        const lease = await acquireCrossTabLease(name);
        try {
          return await callback(lease);
        } catch (error) {
          if (error && typeof error === "object" && !lease.isHeld()) {
            error.leaseLost = true;
          }
          throw error;
        } finally {
          lease.release();
        }
      });
    const settled = run.then(
      () => {},
      () => {},
    );
    inTabLocks.set(name, settled);
    settled.then(() => {
      if (inTabLocks.get(name) === settled) inTabLocks.delete(name);
    });
    return run;
  }

  return {
    requestMutationSendLock: requestLock,
    notifyLockWaiters,
  };
}

let defaultLocker;
function getDefaultLocker() {
  if (!defaultLocker) {
    defaultLocker = createMutationSendLocker();
  }
  return defaultLocker;
}

/**
 * Serialize sends for a scope across tabs via Web Locks when available, and
 * fall back to an in-tab promise mutex plus a heartbeat-renewed, fenced
 * localStorage/BroadcastChannel lease on non-secure origins (e.g. plain-HTTP
 * LAN or Tailnet).
 */
export function requestMutationSendLock(scope, callback) {
  return getDefaultLocker().requestMutationSendLock(scope, callback);
}

// File selection needs async byte reading. Warn until it has a durable checkpoint.
export function protectLocalWorkPreparation(scope) {
  const token = Symbol();
  preparing.set(token, { scope, epoch: localWorkEpoch(scope) });
  return () => preparing.delete(token);
}
const rememberedOwners = new Map();
const workspaceKey = (scope) => JSON.stringify(JSON.parse(scope).slice(0, 2));

/** Last validated owner, including across a temporary anonymous session. */
export function lastLocalWorkOwner(scope) {
  const key = workspaceKey(scope);
  try {
    const previous = localStorage.getItem(`${PREFIX}owner:${key}`);
    if (
      previous &&
      localWorkPrincipal(previous) &&
      workspaceKey(previous) === key
    )
      return previous;
  } catch {
    /* The IndexedDB marker is read when an owner is adopted. */
  }
  return rememberedOwners.get(key) || null;
}

export function rememberLocalWorkOwner(scope) {
  if (!localWorkPrincipal(scope)) return;
  const key = workspaceKey(scope);
  const previous = lastLocalWorkOwner(scope);
  if (previous === scope) return;
  if (previous && localWorkPrincipal(previous) !== localWorkPrincipal(scope))
    void clearPrincipalLocalWork(previous);
  rememberedOwners.set(key, scope);
  try {
    localStorage.setItem(`${PREFIX}owner:${key}`, scope);
  } catch {
    /* Persist the marker below. */
  }
  void serialized(async () => {
    try {
      const stored = await transaction("readonly", (store) =>
        store.get(`owner:${key}`),
      );
      if (
        !previous &&
        stored?.value &&
        localWorkPrincipal(stored.value) !== localWorkPrincipal(scope)
      ) {
        // Do not await a deletion queued behind this serialized operation.
        void clearPrincipalLocalWork(stored.value);
      }
      await transaction("readwrite", (store) =>
        store.put({
          key: `owner:${key}`,
          scope: "__owners__",
          principal: "",
          value: scope,
        }),
      );
    } catch {
      /* The synchronous marker is sufficient when IndexedDB is denied. */
    }
    notify();
    channel?.postMessage({ changed: `owner:${key}` });
  });
}
const channel =
  typeof window !== "undefined" && window.BroadcastChannel
    ? new window.BroadcastChannel("anx-local-work-v1")
    : null;
const epochKey = (principal) => `${PREFIX}principal:${principal}`;
export function localWorkEpoch(scope) {
  const principal = localWorkPrincipal(scope);
  try {
    const epoch = localStorage.getItem(epochKey(principal));
    if (epoch) epochs.set(principal, epoch);
  } catch {
    /* BroadcastChannel still synchronizes revocation. */
  }
  return epochs.get(principal) || "initial";
}

export function localWorkPrincipal(scope) {
  try {
    const [, , agent, actor] = JSON.parse(scope);
    return agent || actor ? JSON.stringify([agent, actor]) : "";
  } catch {
    return "";
  }
}

export function localWorkKey(scope, kind, target) {
  if (!localWorkPrincipal(scope)) {
    throw new Error("Choose a signed-in identity before saving work.");
  }
  return JSON.stringify([scope, kind, target]);
}

function openDatabase() {
  if (!globalThis.indexedDB) return Promise.reject(new Error("No IndexedDB"));
  if (!database) {
    database = new Promise((resolve, reject) => {
      const request = indexedDB.open(DB_NAME, 1);
      request.onupgradeneeded = () => {
        const store = request.result.createObjectStore("work", {
          keyPath: "key",
        });
        store.createIndex("scope", "scope");
        store.createIndex("principal", "principal");
      };
      request.onsuccess = () => {
        request.result.onversionchange = () => {
          request.result.close();
          database = undefined;
        };
        resolve(request.result);
      };
      request.onerror = () => {
        database = undefined;
        reject(request.error);
      };
      request.onblocked = () => reject(new Error("Local database is blocked"));
    });
  }
  return database;
}

function serialized(operation) {
  const result = operations.then(operation);
  operations = result.catch(() => {});
  return result;
}

async function transaction(mode, operation) {
  const db = await openDatabase();
  return new Promise((resolve, reject) => {
    const tx = db.transaction("work", mode);
    const request = operation(tx.objectStore("work"));
    let result;
    if (request) request.onsuccess = () => (result = request.result);
    tx.oncomplete = () => resolve(result);
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error || new Error("Local save aborted"));
  });
}

function journalRecords() {
  const result = [];
  try {
    for (let i = 0; i < localStorage.length; i += 1) {
      const key = localStorage.key(i);
      if (!key?.startsWith(PREFIX)) continue;
      try {
        const record = JSON.parse(localStorage.getItem(key));
        if (record?.key) result.push(record);
      } catch {
        // A corrupt record must not prevent recovery of other user work.
      }
    }
  } catch {
    // Private browsers may deny localStorage. IndexedDB is still attempted.
  }
  return result;
}

function notify() {
  for (const listener of listeners) listener();
}

if (typeof window !== "undefined") {
  window.addEventListener("beforeunload", (event) => {
    if (
      ![...uncheckpointed.values(), ...preparing.values()].some(
        (entry) => entry.epoch === localWorkEpoch(entry.scope),
      )
    )
      return;
    event.preventDefault();
    event.returnValue = "";
  });
  window.addEventListener("storage", (event) => {
    if (event.key?.startsWith(`${PREFIX}principal:`) && event.newValue) {
      try {
        const current = localStorage.getItem(event.key);
        if (current && current !== event.newValue) return;
        const principal = JSON.parse(
          event.key.slice(`${PREFIX}principal:`.length),
        );
        void clearPrincipalLocalWork(JSON.stringify(["", "", ...principal]), {
          epoch: event.newValue,
          broadcast: false,
        });
      } catch {
        /* Ignore malformed storage events. */
      }
    }
    if (event.key?.startsWith("anx:mutation-lock:"))
      defaultLocker?.notifyLockWaiters();
    if (event.key?.startsWith(PREFIX)) notify();
  });
  if (channel)
    channel.onmessage = (event) => {
      if (event.data?.lockReleased) {
        defaultLocker?.notifyLockWaiters();
        return;
      }
      if (event.data?.purge) {
        // A delayed channel message must not replace a newer revocation.
        const principal = localWorkPrincipal(event.data.scope);
        if (appliedRevocations.get(principal) === event.data.epoch) return;
        let current;
        let storageAvailable = false;
        try {
          current = localStorage.getItem(epochKey(principal));
          storageAvailable = true;
        } catch {
          /* The channel remains usable without localStorage. */
        }
        if (storageAvailable && current !== event.data.epoch) return;
        void clearPrincipalLocalWork(event.data.scope, {
          epoch: event.data.epoch,
          broadcast: false,
        });
      } else notify();
    };
}

export function onLocalWorkChanged(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** The journal write happens before this function returns. Never debounce it.
 * The promise confirms at least one durable backend accepted the write. */
export function saveLocalWork(
  scope,
  kind,
  target,
  value,
  {
    epoch = localWorkEpoch(scope),
    deletionReason = "",
    confirmation = null,
    submission = null,
    lease = null,
  } = {},
) {
  if (epoch !== localWorkEpoch(scope)) {
    throw new Error("The identity that authored this work has signed out.");
  }
  if (lease) {
    lease.assertHeld();
  }
  const key = localWorkKey(scope, kind, target);
  lastWrite = Math.max(Date.now(), lastWrite + 1);
  const record = {
    key,
    scope,
    principal: localWorkPrincipal(scope),
    kind,
    target,
    value,
    updatedAt: lastWrite,
    deleted: value === null,
    deletionReason,
    confirmation,
    submission,
    epoch,
  };
  let journalSaved = false;
  let storedRecord = record;
  try {
    const checkpoint = JSON.stringify(record);
    // Svelte state proxies are not structured-cloneable. The request/draft
    // values here are JSON; freeze the same representation as the journal.
    storedRecord = JSON.parse(checkpoint);
    localStorage.setItem(PREFIX + key, checkpoint);
    journalSaved = true;
  } catch {
    // IndexedDB is the primary backend and can succeed despite journal quota.
  }
  const token = Symbol();
  if (journalSaved) uncheckpointed.delete(key);
  else uncheckpointed.set(key, { scope, epoch, token, record: storedRecord });
  const saved = serialized(async () => {
    if (epoch !== localWorkEpoch(scope)) {
      throw new Error("The identity that authored this work has signed out.");
    }
    if (lease) {
      lease.assertHeld();
    }
    try {
      await transaction("readwrite", (store) => store.put(storedRecord));
    } catch {
      if (!journalSaved) {
        const error = new Error(
          "Local saving is unavailable. Keep this tab open and copy your text before leaving.",
        );
        error.localPersistence = true;
        throw error;
      }
    }
    notify();
    channel?.postMessage({ changed: key });
    return record;
  });
  void saved.then(
    () => {
      if (uncheckpointed.get(key)?.token === token) uncheckpointed.delete(key);
    },
    () => {},
  );
  // Callers may retain this promise until send, but must never leak a rejection.
  void saved.catch(() => {});
  return saved;
}

export async function listLocalWork(
  scope,
  kind,
  { includeDeleted = false } = {},
) {
  await operations;
  let stored = [];
  try {
    stored = await transaction("readonly", (store) =>
      store.index("scope").getAll(scope),
    );
  } catch {
    // Read the synchronous fallback below.
  }
  const merged = new Map(stored.map((record) => [record.key, record]));
  for (const record of journalRecords()) {
    if (record.scope !== scope) continue;
    const previous = merged.get(record.key);
    if (!previous || record.updatedAt >= previous.updatedAt)
      merged.set(record.key, record);
  }
  // Failed disk writes still survive navigation in this tab. They remain
  // uncheckpointed, so sending is blocked and closing the tab still warns.
  for (const { record } of uncheckpointed.values()) {
    if (!record || record.scope !== scope) continue;
    const previous = merged.get(record.key);
    if (!previous || record.updatedAt >= previous.updatedAt)
      merged.set(record.key, record);
  }
  return [...merged.values()].filter(
    (record) =>
      record.epoch === localWorkEpoch(scope) &&
      (includeDeleted || !record.deleted) &&
      (!kind || record.kind === kind),
  );
}

export async function loadLocalWorkRecord(scope, kind, target) {
  const key = localWorkKey(scope, kind, target);
  await operations;
  let stored = null;
  try {
    stored = await transaction("readonly", (store) => store.get(key));
  } catch {
    /* Read the journal. */
  }
  try {
    const journal = JSON.parse(localStorage.getItem(PREFIX + key));
    if (
      journal?.key === key &&
      (!stored || journal.updatedAt >= stored.updatedAt)
    )
      stored = journal;
  } catch {
    /* IndexedDB can still serve this record. */
  }
  const memory = uncheckpointed.get(key)?.record;
  if (memory && (!stored || memory.updatedAt >= stored.updatedAt))
    stored = memory;
  return stored?.epoch === localWorkEpoch(scope) ? stored : null;
}

export async function loadLocalWork(scope, kind, target) {
  const record = await loadLocalWorkRecord(scope, kind, target);
  return record && !record.deleted ? record.value : null;
}

/** Sign-out/principal switch is the explicit privacy boundary. Serialized
 * deletion runs after queued saves, so an old save cannot resurrect a draft. */
export function clearPrincipalLocalWork(
  scope,
  { epoch = randomUUID(), broadcast = true } = {},
) {
  const principal = localWorkPrincipal(scope);
  if (!principal) return Promise.resolve();
  if (!broadcast && appliedRevocations.get(principal) === epoch)
    return Promise.resolve();
  appliedRevocations.set(principal, epoch);
  for (const [key, entry] of uncheckpointed) {
    if (localWorkPrincipal(entry.scope) === principal)
      uncheckpointed.delete(key);
  }
  // Old Inbox drafts had no principal. The explicit privacy boundary clears
  // those unattributed copies too; never assign them to the next reader.
  try {
    for (let i = localStorage.length - 1; i >= 0; i -= 1) {
      const key = localStorage.key(i);
      if (key?.startsWith("anx.human-response.draft:"))
        localStorage.removeItem(key);
    }
  } catch {
    /* IndexedDB cleanup still runs below. */
  }
  try {
    for (let i = sessionStorage.length - 1; i >= 0; i -= 1) {
      const key = sessionStorage.key(i);
      if (key?.startsWith("anx.pm.draft:")) sessionStorage.removeItem(key);
    }
  } catch {
    /* Legacy session copies are never automatically restored. */
  }
  epochs.set(principal, epoch);
  try {
    localStorage.setItem(epochKey(principal), epoch);
  } catch {
    /* Use the channel below. */
  }
  if (broadcast) channel?.postMessage({ purge: true, scope, epoch });
  if (typeof window !== "undefined")
    window.dispatchEvent(
      new CustomEvent("anx:local-work-purged", { detail: { principal } }),
    );
  for (const record of journalRecords()) {
    if (record.principal === principal && record.epoch !== epoch) {
      try {
        localStorage.removeItem(PREFIX + record.key);
      } catch {
        // IndexedDB cleanup below still applies.
      }
    }
  }
  return serialized(async () => {
    try {
      await transaction("readwrite", (store) => {
        const request = store.index("principal").openCursor(principal);
        request.onsuccess = () => {
          const cursor = request.result;
          if (cursor) {
            // Work checkpointed after this revocation belongs to the new
            // session; repeated cross-tab cleanup must preserve it.
            if (cursor.value.epoch !== epoch) cursor.delete();
            cursor.continue();
          }
        };
        // Cursor callbacks own this request.
      });
    } finally {
      notify();
    }
  }).catch(() => {});
}
