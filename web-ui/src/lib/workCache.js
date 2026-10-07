/**
 * Short-lived card snapshots, so opening a task is not a blank page.
 *
 * Opening a card used to cost four serial hops before anything was on screen:
 * `/auth/session`, then `work.get`, then the plan, then the refs. The reader
 * had already seen the card's title, owner and status in the list they clicked
 * — the page just threw that away and asked for it again.
 *
 * Two things live here:
 *
 * - **Primed summaries.** A list writes the row it already has. The detail
 *   page paints it at once and marks the view partial while the real read is
 *   in flight. Nothing is written from a primed summary, and a write path
 *   never reads one: it is display only.
 * - **Prefetched records.** Hovering a link reads the card once, early, so the
 *   click has nothing left to wait for.
 *
 * Browser-only, memory-only, 30 seconds, bounded, and never an authorization
 * source — the same contract `workspaceViewCache` holds for page snapshots.
 */

const TTL_MS = 30_000;
const MAX_ENTRIES = 60;

/** @type {Map<string, { work: object, full: boolean, at: number }>} */
const entries = new Map();
/** Reads in flight, so a pointer sweeping a board asks for each card once. */
const inflight = new Map();

const asText = (value) => String(value ?? "").trim();

/**
 * Every handle the router might name this card by. `/tasks/{workId}` takes a
 * ref, a bare handle or an id, so a snapshot primed from a list row has to be
 * findable under all three.
 */
export function workCacheKeys(work) {
  const ref = asText(work?.ref);
  return [
    ref,
    ref.replace(/^card:/, ""),
    asText(work?.handle),
    asText(work?.id),
  ].filter(Boolean);
}

function prune(now) {
  for (const [key, entry] of entries) {
    if (now - entry.at >= TTL_MS) entries.delete(key);
  }
  while (entries.size > MAX_ENTRIES) {
    entries.delete(entries.keys().next().value);
  }
}

function write(work, full, now) {
  if (typeof window === "undefined" || !work) return;
  const keys = workCacheKeys(work);
  if (!keys.length) return;
  prune(now);
  for (const key of keys) {
    const existing = entries.get(key);
    // A full record is never downgraded to a list row by a later prime.
    if (existing?.full && !full) continue;
    entries.delete(key);
    entries.set(key, { work, full, at: now });
  }
}

/** Write a list row, for the detail page to paint while it reads the real one. */
export function primeWorkSummary(work, now = Date.now()) {
  write(work, false, now);
}

/** Write a real `work.get` result. */
export function cacheWorkRecord(work, now = Date.now()) {
  write(work, true, now);
}

/**
 * What is known about `id` right now, or null.
 *
 * `full` says whether this came from a card read or from a list row; a caller
 * paints either and keeps reading when it is not full.
 *
 * @returns {null | { work: object, full: boolean }}
 */
export function readWorkSnapshot(id, now = Date.now()) {
  if (typeof window === "undefined") return null;
  const key = asText(id);
  if (!key) return null;
  const entry = entries.get(key) ?? entries.get(key.replace(/^card:/, ""));
  if (!entry) return null;
  if (now - entry.at >= TTL_MS) {
    for (const candidate of workCacheKeys(entry.work))
      entries.delete(candidate);
    return null;
  }
  return { work: entry.work, full: entry.full };
}

/**
 * Read `id` now because the pointer is on its link.
 *
 * Costs nothing when the card is already cached or already being read, so it
 * is safe to call on every `pointerenter` of every row.
 *
 * @param {string} id
 * @param {{ getWork: (id: string) => Promise<{work?: object}> }} client
 */
export function prefetchWork(id, client) {
  const key = asText(id);
  if (!key || typeof window === "undefined") return Promise.resolve(null);
  if (readWorkSnapshot(key)?.full) return Promise.resolve(null);
  const existing = inflight.get(key);
  if (existing) return existing;
  const promise = Promise.resolve()
    .then(() => client.getWork(key))
    .then((result) => {
      const work = result?.work ?? null;
      if (work) cacheWorkRecord(work);
      return work;
    })
    .catch(() => null)
    .finally(() => inflight.delete(key));
  inflight.set(key, promise);
  return promise;
}

/** Test hook, and what a sign-out clears. */
export function clearWorkCache() {
  entries.clear();
  inflight.clear();
}
