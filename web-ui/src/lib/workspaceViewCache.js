// Browser-only, short-lived display snapshots. Never an authorization source.
const entries = new Map();
const TTL_MS = 30_000;
const MAX_ENTRIES = 12;

export function readWorkspaceView(key, now = Date.now()) {
  if (typeof window === "undefined") return null;
  const entry = entries.get(key);
  if (!entry || now - entry.at >= TTL_MS) {
    entries.delete(key);
    return null;
  }
  return entry.value;
}

export function writeWorkspaceView(key, value, now = Date.now()) {
  if (typeof window === "undefined") return;
  entries.delete(key);
  entries.set(key, { value, at: now });
  while (entries.size > MAX_ENTRIES)
    entries.delete(entries.keys().next().value);
}

export function clearWorkspaceViews() {
  entries.clear();
}
