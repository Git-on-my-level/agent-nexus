import { get } from "svelte/store";
import { coreClient } from "$lib/coreClient";
import { askRefForInboxItem } from "$lib/askDelivery.js";
import { authenticatedAgent } from "$lib/authSession.js";
import { selectedActorId } from "$lib/actorSession.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "$lib/workspaceContext.js";

/**
 * The ask behind one inbox item: its durable outcome and its own event.
 *
 * Two bounded point reads per selected item, and only for the item the reader
 * has selected — never one per row. `asks.get` is indexed point lookups over
 * the ask event, its resolution and at most 32 delivery rows; `events.get` is
 * a single event by id. Neither grows with the workspace, and the Handled list
 * itself makes no extra request, which is what keeps a page of answered items
 * at the cost of the page rather than the cost of its rows.
 *
 * The event read is what carries authoring evidence — `payload.evidence` and
 * `payload.supersedes` are not projected onto the inbox row. A refused or
 * missing read is not an error here: the panel falls back to the row's own
 * `related_refs`, which already merge the event's native refs.
 *
 * ## What a cached read belongs to
 *
 * Core applies the caller's resource access on every statement of these reads,
 * so a cached result belongs to the reader and workspace it was read for and to
 * nobody else. The cache is keyed by that identity and dropped when it changes,
 * the same scope `inboxResponseQueue` keeps for a response in flight. Within
 * one identity a read is only good for as long as it is plausibly current:
 * delivery state moves on its own (a webhook retries, a bridge acknowledges),
 * and answering an ask replaces its outcome outright.
 */

const CACHE_TTL_MS = 30_000;

/** @type {Map<string, { at: number, promise: Promise<object> }>} */
const cache = new Map();

let scopeIdentity = "";

/**
 * The reader and workspace a cached read belongs to. Read at call time rather
 * than subscribed: a store subscription here would run during module load in
 * every context that imports this file, and the only thing it would buy is
 * dropping the cache a moment earlier than the next read does.
 */
function currentScope() {
  const agent = get(authenticatedAgent);
  return JSON.stringify([
    get(currentOrganizationSlug),
    get(currentWorkspaceSlug),
    agent?.agent_id || "",
    agent?.actor_id || get(selectedActorId) || "",
  ]);
}

function cacheForCurrentScope() {
  const identity = currentScope();
  if (identity !== scopeIdentity) {
    scopeIdentity = identity;
    cache.clear();
  }
  return cache;
}

function eventIdFromRef(ref) {
  const raw = String(ref ?? "").trim();
  return raw.startsWith("event:") ? raw.slice("event:".length).trim() : "";
}

/**
 * @param {object} item an inbox item (open or completed)
 * @param {{ client?: object }} [options]
 * @returns {Promise<{ askRef: string, outcome: object | null, event: object | null }>}
 */
export function loadAskDetail(item, { client = coreClient } = {}) {
  const askRef = askRefForInboxItem(item);
  if (!askRef) {
    return Promise.resolve({ askRef: "", outcome: null, event: null });
  }
  const scoped = cacheForCurrentScope();
  const cached = scoped.get(askRef);
  if (cached && Date.now() - cached.at < CACHE_TTL_MS) return cached.promise;
  const promise = (async () => {
    const [outcome, event] = await Promise.all([
      Promise.resolve()
        .then(() => client.getAsk(askRef))
        .then((value) => (value && typeof value === "object" ? value : null))
        .catch(() => null),
      Promise.resolve()
        .then(() => client.getEvent(eventIdFromRef(askRef)))
        .then((value) => value?.event ?? value ?? null)
        .catch(() => null),
    ]);
    return { askRef, outcome, event };
  })();
  const entry = { at: Date.now(), promise };
  scoped.set(askRef, entry);
  // A read that returned nothing is retried on the next selection rather than
  // waiting out the window: both calls can fail for reasons that pass (a
  // dropped connection). Only this entry is dropped — a newer one may have
  // replaced it, or the scope may have changed, while this read was in flight.
  const forget = () => {
    if (cache.get(askRef) === entry) cache.delete(askRef);
  };
  promise.then((value) => {
    if (!value.outcome && !value.event) forget();
  }, forget);
  return promise;
}

/**
 * Drops cached ask reads so the next selection sees new delivery state.
 *
 * With an inbox item, drops only that ask — what an answer invalidates is the
 * ask it answered. The open row and the Handled row it becomes are the same
 * ask, so without this the Handled row could show the pre-answer outcome.
 */
export function invalidateAskDetail(item = null) {
  if (!item) {
    cache.clear();
    return;
  }
  const askRef = askRefForInboxItem(item);
  if (askRef) cache.delete(askRef);
}

/** Test hook: forget every cached read and the identity it was read for. */
export function resetAskDetailCache() {
  cache.clear();
  scopeIdentity = "";
}
