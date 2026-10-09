// Display snapshots only; every read still authenticates and revalidates.
// One versioned, size-bounded storage value avoids scanning browser storage.
const entries = new Map();
const STORAGE_KEY = "anx.workspace-views.v1";
const TTL_MS = 24 * 60 * 60 * 1000;
const MAX_ENTRIES = 12;
const MAX_BYTES = 2_000_000;
let hydrated = false;

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
  entries.clear();
  hydrated = true;
  try {
    window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* Optional. */
  }
}
