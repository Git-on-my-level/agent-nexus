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
const uncheckpointed = new Map();
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
      ![...uncheckpointed.values()].some(
        (entry) => entry.epoch === localWorkEpoch(entry.scope),
      )
    )
      return;
    event.preventDefault();
    event.returnValue = "";
  });
  window.addEventListener("storage", (event) => {
    if (event.key?.startsWith(PREFIX)) notify();
  });
  if (channel)
    channel.onmessage = (event) => {
      if (event.data?.purge) {
        // A delayed channel message must not replace a newer revocation.
        const principal = localWorkPrincipal(event.data.scope);
        if (epochs.get(principal) === event.data.epoch) return;
        let current;
        try {
          current = localStorage.getItem(epochKey(principal));
        } catch {
          /* The channel remains usable without localStorage. */
        }
        if (current && current !== event.data.epoch) return;
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
  } = {},
) {
  if (epoch !== localWorkEpoch(scope)) {
    throw new Error("The identity that authored this work has signed out.");
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

export async function listLocalWork(scope, kind) {
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
      !record.deleted &&
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
  { epoch = crypto.randomUUID(), broadcast = true } = {},
) {
  const principal = localWorkPrincipal(scope);
  if (!principal) return Promise.resolve();
  if (!broadcast && epochs.get(principal) === epoch) return Promise.resolve();
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
