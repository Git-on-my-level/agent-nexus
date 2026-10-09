// Display snapshots only; every read still authenticates and revalidates.
// One versioned, size-bounded storage value avoids scanning browser storage.
const entries = new Map();
const STORAGE_KEY = "anx.workspace-views.v1";
const TTL_MS = 24 * 60 * 60 * 1000;
const MAX_ENTRIES = 12;
const MAX_BYTES = 2_000_000;
let hydrated = false;
let revision = 0;
const listeners = new Set();
const deniedListeners = new Map();

export function onWorkspaceViewsDenied(scope, listener) {
  const scoped = deniedListeners.get(scope) || new Set();
  deniedListeners.set(scope, scoped);
  scoped.add(listener);
  return () => {
    scoped.delete(listener);
    if (!scoped.size) deniedListeners.delete(scope);
  };
}

export const workspaceViewRevision = () => revision;

export function onWorkspaceViewChanged(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Confirmed mutations invalidate reads started before the confirmation. */
export function reviseWorkspaceView(key, transform) {
  const previous = readWorkspaceView(key);
  revision++;
  const next = transform(previous);
  if (next) writeWorkspaceView(key, next);
  else {
    entries.delete(key);
    persist();
  }
  for (const listener of listeners) {
    try {
      listener(key, next);
    } catch {
      /* A display subscriber cannot undo confirmation. */
    }
  }
}

/** A denial revokes all display snapshots for this workspace and principal. */
export function purgeWorkspaceViews(scope, error) {
  hydrate();
  revision++;
  for (const key of new Set([...entries.keys(), `${scope}:inbox`])) {
    if (key.startsWith(`${scope}:`)) reviseWorkspaceView(key, () => null);
  }
  for (const listener of deniedListeners.get(scope) || []) {
    try {
      listener(error);
    } catch {
      /* Continue revoking other mounted views. */
    }
  }
}

function hydrate() {
  if (hydrated || typeof window === "undefined") return;
  hydrated = true;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw || raw.length > MAX_BYTES) return;
    const stored = JSON.parse(raw);
    if (stored.version !== 1 || !Array.isArray(stored.entries)) return;
    for (const [key, entry] of stored.entries.slice(-MAX_ENTRIES)) {
      if (typeof key === "string" && Number.isFinite(entry?.at))
        entries.set(key, entry);
    }
  } catch {
    /* Storage disabled, corrupt or exhausted: memory still works. */
  }
}

function persist() {
  try {
    let raw = JSON.stringify({ version: 1, entries: [...entries] });
    while (raw.length > MAX_BYTES && entries.size) {
      entries.delete(entries.keys().next().value);
      raw = JSON.stringify({ version: 1, entries: [...entries] });
    }
    window.localStorage.setItem(STORAGE_KEY, raw);
  } catch {
    /* Display caching must never prevent navigation. */
  }
}

export function readWorkspaceView(key, now = Date.now()) {
  if (typeof window === "undefined") return null;
  hydrate();
  const entry = entries.get(key);
  if (!entry || now - entry.at >= TTL_MS) {
    entries.delete(key);
    return null;
  }
  return entry.value;
}

export function writeWorkspaceView(key, value, now = Date.now()) {
  if (typeof window === "undefined") return;
  hydrate();
  entries.delete(key);
  entries.set(key, { value, at: now });
  while (entries.size > MAX_ENTRIES)
    entries.delete(entries.keys().next().value);
  persist();
}

export function clearWorkspaceViews() {
  revision++;
  entries.clear();
  hydrated = true;
  try {
    window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* Optional. */
  }
}
