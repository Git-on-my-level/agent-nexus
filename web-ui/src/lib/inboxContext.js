import { coreClient } from "$lib/coreClient";
import { splitTypedRef } from "$lib/inboxUtils.js";

/**
 * Context for one inbox item: what it blocks and the latest progress note
 * around it, read from existing endpoints (`docs.get`, `events.list`).
 *
 * The note is the requester's own latest message when there is one — that is
 * the "where was the agent when it stopped" line — otherwise the newest
 * message on the threads the item names.
 */

const MAX_THREADS = 4;
const MESSAGES_PER_THREAD = 10;
const EXCERPT_LENGTH = 180;

function text(value) {
  return String(value ?? "").trim();
}

export function excerpt(value, length = EXCERPT_LENGTH) {
  const flat = text(value).replace(/\s+/g, " ");
  return flat.length > length
    ? `${flat.slice(0, length - 1).trimEnd()}…`
    : flat;
}

/** Backing threads worth reading for an item, most specific first. */
export function contextThreads(item, subject) {
  const out = [];
  const add = (value) => {
    const raw = text(value);
    if (!raw) return;
    const ref = raw.startsWith("thread:") ? raw : `thread:${raw}`;
    if (!out.includes(ref)) out.push(ref);
  };
  if (subject?.kind === "card") {
    // A task's discussion lives on its own backing thread, which shares the
    // task's handle.
    add(subject.work?.handle || splitTypedRef(subject.ref).id);
  }
  if (subject?.kind === "document" && subject.threadId) add(subject.threadId);
  for (const ref of Array.isArray(item?.related_refs)
    ? item.related_refs
    : []) {
    if (text(ref).startsWith("thread:")) add(ref);
  }
  add(item?.thread_id);
  return out.slice(0, MAX_THREADS);
}

/** Picks the note to show from message events (any order). */
export function pickProgressNote(events, requesterId = "") {
  const seen = new Set();
  const messages = (Array.isArray(events) ? events : [])
    .filter((event) => {
      const id = text(event?.id);
      if (!id || seen.has(id) || event?.type !== "message_posted") return false;
      seen.add(id);
      return Boolean(text(event?.payload?.text));
    })
    .sort((a, b) => Date.parse(b?.ts ?? "") - Date.parse(a?.ts ?? ""));
  const requester = text(requesterId);
  const own = requester
    ? messages.find((event) => text(event?.actor_id) === requester)
    : null;
  const chosen = own || messages[0];
  if (!chosen) return null;
  return {
    id: text(chosen.id),
    actorId: text(chosen.actor_id),
    ts: text(chosen.ts),
    text: excerpt(chosen.payload.text),
    byRequester: Boolean(own),
  };
}

const cache = new Map();

/**
 * @param {object} item the inbox item
 * @param {object | null} subject from `inboxItemSubject`
 * @returns {Promise<{ note: object | null, document: object | null }>}
 */
export async function loadInboxContext(item, subject) {
  const key = `${text(item?.id)}|${text(subject?.ref)}`;
  if (cache.has(key)) return cache.get(key);
  const promise = (async () => {
    let document = null;
    let threadId = "";
    if (subject?.kind === "document") {
      try {
        const response = await coreClient.getDocument(
          splitTypedRef(subject.ref).id,
        );
        document = response?.document ?? null;
        threadId = text(document?.thread_id);
      } catch {
        document = null;
      }
    }
    const threads = contextThreads(item, { ...subject, threadId });
    const pages = await Promise.all(
      threads.map((thread_id) =>
        Promise.resolve()
          .then(() =>
            coreClient.listEvents({
              thread_id,
              type: "message_posted",
              limit: MESSAGES_PER_THREAD,
            }),
          )
          .then((result) =>
            Array.isArray(result?.events) ? result.events : [],
          )
          .catch(() => []),
      ),
    );
    const note = pickProgressNote(
      pages.flat(),
      text(item?.requester_actor_id) || text(item?.requester_agent_id),
    );
    return { note, document };
  })();
  cache.set(key, promise);
  // A failed or empty read is retried on the next selection, not pinned.
  promise.then(
    (value) => {
      if (!value.note && !value.document) cache.delete(key);
    },
    () => cache.delete(key),
  );
  return promise;
}

/** Drops cached context so the next read sees new messages. */
export function invalidateInboxContext() {
  cache.clear();
}
