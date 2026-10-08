import { coreClient } from "$lib/coreClient";
import { askRefForInboxItem } from "$lib/askDelivery.js";

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
 */

const cache = new Map();

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
  const cached = cache.get(askRef);
  if (cached) return cached;
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
  cache.set(askRef, promise);
  // A read that returned nothing is retried on the next selection rather than
  // pinned: both calls can fail for reasons that pass (a dropped connection).
  promise.then(
    (value) => {
      if (!value.outcome && !value.event) cache.delete(askRef);
    },
    () => cache.delete(askRef),
  );
  return promise;
}

/** Drops cached ask reads so the next selection sees new delivery state. */
export function invalidateAskDetail() {
  cache.clear();
}
