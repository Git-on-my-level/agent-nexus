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
 *
 * ## Every entry is scoped to the workspace it was read under
 *
 * Card handles are unique within a workspace, not across them: `card:release`
 * exists in both. Keyed on the handle alone, reading a card in one workspace
 * and opening the same handle in another painted the first workspace's title
 * and body under the second one's name while the real read was in flight —
 * showing a reader content from a workspace they had navigated away from.
 *
 * The scope is resolved the same way `coreClient` resolves its routing headers
 * (`coreClientRequestHeaders.js`): the workspace context store, falling back to
 * the URL. Deriving it the same way is the point — an entry can then only be
 * read back under exactly the routing scope that produced it, so the cache
 * cannot answer a question the request would have sent somewhere else.
 */

import {
  getCurrentOrganizationSlug,
  getCurrentWorkspaceSlug,
} from "$lib/workspaceContext";
import { APP_BASE_PATH, parseWorkspaceRouteSlugs } from "$lib/workspacePaths";

const TTL_MS = 30_000;
const MAX_ENTRIES = 60;

/** @type {Map<string, { work: object, full: boolean, at: number }>} */
const entries = new Map();
/** Reads in flight, so a pointer sweeping a board asks for each card once. */
const inflight = new Map();

const asText = (value) => String(value ?? "").trim();

/**
 * The workspace this browser is currently talking to, as `org/workspace`.
 *
 * Resolved exactly as `buildCoreRequestContextHeaders` resolves it, so the
 * cache's scope and the request's routing can never disagree. An unresolvable
 * scope returns `""`, which {@link scoped} treats as "do not cache": a read
 * nobody can attribute to a workspace is not one to hand back later.
 */
export function workCacheScope() {
  const org = asText(getCurrentOrganizationSlug());
  const workspace = asText(getCurrentWorkspaceSlug());
  if (org && workspace) return `${org}/${workspace}`;
  const fromUrl = parseWorkspaceRouteSlugs(
    globalThis.location?.pathname ?? "/",
    APP_BASE_PATH,
  );
  const urlOrg = org || asText(fromUrl.organizationSlug);
  const urlWorkspace = workspace || asText(fromUrl.workspaceSlug);
  return urlOrg && urlWorkspace ? `${urlOrg}/${urlWorkspace}` : "";
}

/** A key namespaced to its workspace, or "" when there is no workspace. */
function scoped(key, scope) {
  const name = asText(key);
  return scope && name ? `${scope}\u0000${name}` : "";
}

/**
 * Every handle the router might name this card by. `/tasks/{workId}` takes a
 * ref, a bare handle or an id, so a snapshot primed from a list row has to be
 * findable under all three.
 *
 * These are the unscoped names; {@link scoped} prefixes the workspace.
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
  const scope = workCacheScope();
  // No resolvable workspace, nothing to attribute the card to: do not cache.
  if (!scope) return;
  const keys = workCacheKeys(work);
  if (!keys.length) return;
  prune(now);
  for (const name of keys) {
    const key = scoped(name, scope);
    const existing = entries.get(key);
    /*
     * Within one workspace a full record is strictly more than a list row, so
     * a later prime never downgrades it. Across workspaces this cannot arise:
     * the same handle in two workspaces is two keys.
     */
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
  const scope = workCacheScope();
  const name = asText(id);
  if (!scope || !name) return null;
  const entry =
    entries.get(scoped(name, scope)) ??
    entries.get(scoped(name.replace(/^card:/, ""), scope));
  if (!entry) return null;
  if (now - entry.at >= TTL_MS) {
    for (const candidate of workCacheKeys(entry.work)) {
      entries.delete(scoped(candidate, scope));
    }
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
  const name = asText(id);
  if (!name || typeof window === "undefined") return Promise.resolve(null);
  const scope = workCacheScope();
  if (!scope) return Promise.resolve(null);
  if (readWorkSnapshot(name)?.full) return Promise.resolve(null);
  // In flight is scoped too: the same handle in two workspaces is two reads.
  const key = scoped(name, scope);
  const existing = inflight.get(key);
  if (existing) return existing;
  const promise = Promise.resolve()
    .then(() => client.getWork(name))
    .then((result) => {
      const work = result?.work ?? null;
      /*
       * File it under the scope the read was issued in, not whatever is
       * current now: a reader who switched workspaces mid-flight must not have
       * the answer to the old question filed against the new one.
       */
      if (work && workCacheScope() === scope) cacheWorkRecord(work);
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
