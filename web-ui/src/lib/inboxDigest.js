import { markdownExcerpt } from "$lib/markdown.js";

/**
 * Operator-language digests for Watching update rows.
 *
 * Core groups unread activity by its own nouns (board, topic, thread). An
 * operator does not care which primitive changed; they care who did what:
 * "Leo moved 2 tasks to review · Nina commented". Everything here is built
 * from the events `home.unread` already returned with each group.
 */

const COLUMN_PHRASES = {
  backlog: "backlog",
  ready: "ready",
  in_progress: "in progress",
  blocked: "blocked",
  review: "review",
  done: "done",
  cancelled: "cancelled",
};

const ASK_VERBS = {
  ask: "asked for a decision",
  review: "asked for a review",
  escalate: "escalated",
};

function text(value) {
  return String(value ?? "").trim();
}

/**
 * A message body as one line of prose. Comments are authored markdown, so the
 * raw source leaks syntax into a digest row (`**Blocked:** waiting…`).
 */
function prose(value) {
  return markdownExcerpt(text(value), { limit: 240 });
}

function refsOf(event) {
  return Array.isArray(event?.refs) ? event.refs.map(text) : [];
}

function firstRef(event, prefix) {
  return refsOf(event).find((ref) => ref.startsWith(`${prefix}:`)) || "";
}

/** Strips a core summary prefix such as "Card created: ". */
function afterColon(summary) {
  const raw = text(summary);
  const index = raw.indexOf(": ");
  return index > 0 ? raw.slice(index + 2).trim() : "";
}

/**
 * First name of a person-shaped label ("Leo Park" → "Leo"). Anything that
 * does not look like a person's name (a handle, "codex on m5-mbp") is kept
 * whole, because a truncated handle identifies nobody.
 */
export function shortActorName(label) {
  const raw = text(label);
  // "Maya Chen (Studio producer)": the role is not part of the name.
  const name = raw.replace(/\s*\([^)]*\)\s*$/, "");
  const words = name.split(/\s+/);
  if (
    words.length >= 2 &&
    words.length <= 3 &&
    words.every((word) => /^[A-Z][\p{L}'’-]*$/u.test(word))
  ) {
    return words[0];
  }
  return raw;
}

/**
 * One event in operator vocabulary.
 *
 * @returns {{ key: string, actorId: string, verb: string, plural: string,
 *   objectRef: string, objectTitle: string, excerpt: string, ts: string }}
 *   `verb` reads with a single object ("moved Bug bash to review"), `plural`
 *   takes a count ("moved {n} tasks to review"). `key` groups events that
 *   the digest can count together.
 */
export function describeUpdateEvent(event, { titleFor = () => "" } = {}) {
  const type = text(event?.type);
  const payload =
    event?.payload && typeof event.payload === "object" ? event.payload : {};
  const actorId = text(event?.actor_id);
  const ts = text(event?.ts);
  const cardRef = firstRef(event, "card");
  const documentRef = firstRef(event, "document");
  const cardTitle =
    titleFor(cardRef) ||
    text(payload.subject_title) ||
    (type === "card_created" ? afterColon(event?.summary) : "");
  const docTitle = titleFor(documentRef) || text(payload.subject_title);
  const base = { actorId, ts, excerpt: "" };

  if (type === "card_created") {
    return {
      ...base,
      key: "card_created",
      verb: "created",
      plural: "created {n} tasks",
      objectRef: cardRef,
      objectTitle: cardTitle,
    };
  }
  if (type === "card_moved") {
    const column = text(payload.column_key);
    const to = COLUMN_PHRASES[column] || column.replace(/_/g, " ");
    return {
      ...base,
      key: `card_moved:${column}`,
      verb: to ? `moved {object} to ${to}` : "moved",
      plural: to ? `moved {n} tasks to ${to}` : "moved {n} tasks",
      objectRef: cardRef,
      objectTitle: cardTitle,
    };
  }
  if (type === "card_resolved") {
    return {
      ...base,
      key: "card_resolved",
      verb: "resolved",
      plural: "resolved {n} tasks",
      objectRef: cardRef,
      objectTitle: cardTitle,
    };
  }
  if (type.startsWith("card_")) {
    const done = type.slice("card_".length).replace(/_/g, " ");
    const verb =
      {
        updated: "updated",
        archived: "archived",
        trashed: "moved to trash",
        restored: "restored",
        purged: "permanently deleted",
      }[done] || `changed`;
    return {
      ...base,
      key: `card:${verb}`,
      verb,
      plural: `${verb} {n} tasks`,
      objectRef: cardRef,
      objectTitle: cardTitle,
    };
  }
  if (type === "message_posted") {
    return {
      ...base,
      key: "message_posted",
      verb: "commented",
      plural: "left {n} comments",
      objectRef: "",
      objectTitle: "",
      excerpt: prose(payload.text),
    };
  }
  if (type.startsWith("document_")) {
    const created = type === "document_created";
    return {
      ...base,
      key: created ? "document_created" : "document_changed",
      verb: created ? "created" : "revised",
      plural: created ? "created {n} docs" : "revised {n} docs",
      objectRef: documentRef,
      objectTitle: docTitle,
    };
  }
  if (type === "human_attention_requested") {
    const kind = text(payload.kind).toLowerCase();
    return {
      ...base,
      key: `ask:${kind}`,
      verb: ASK_VERBS[kind] || "asked for attention",
      plural: `${ASK_VERBS[kind] || "asked for attention"} {n} times`,
      objectRef: "",
      objectTitle: "",
      excerpt: afterColon(event?.summary) || text(event?.summary),
    };
  }
  if (type === "human_attention_responded") {
    return {
      ...base,
      key: "answered",
      verb: "answered an ask",
      plural: "answered {n} asks",
      objectRef: "",
      objectTitle: "",
    };
  }
  if (type === "topic_updated") {
    return {
      ...base,
      key: "topic_updated",
      verb: "updated the project",
      plural: "updated the project",
      objectRef: "",
      objectTitle: "",
    };
  }
  if (type === "receipt_added") {
    return {
      ...base,
      key: "receipt_added",
      verb: "added a receipt",
      plural: "added {n} receipts",
      objectRef: cardRef,
      objectTitle: cardTitle,
    };
  }
  if (type === "review_completed") {
    return {
      ...base,
      key: "review_completed",
      verb: "completed a review",
      plural: "completed {n} reviews",
      objectRef: cardRef,
      objectTitle: cardTitle,
    };
  }
  if (type === "exception_raised") {
    return {
      ...base,
      key: "exception_raised",
      verb: "reported a problem",
      plural: "reported {n} problems",
      objectRef: cardRef,
      objectTitle: cardTitle,
      excerpt: text(event?.summary),
    };
  }
  return {
    ...base,
    key: `other:${type}`,
    verb: "made a change",
    plural: "made {n} changes",
    objectRef: "",
    objectTitle: "",
  };
}

/** A single described event as a sentence fragment, without the actor. */
export function eventPhrase(described) {
  const verb = String(described?.verb ?? "");
  const title = text(described?.objectTitle);
  if (verb.includes("{object}")) {
    return verb.replace("{object}", title || "a task");
  }
  return title ? `${verb} ${title}` : verb;
}

/**
 * The phrase split around its object, so the object can be a link:
 * "moved " + "Bug bash" + " to review".
 */
export function eventPhraseParts(described) {
  const verb = String(described?.verb ?? "");
  const title = text(described?.objectTitle);
  if (!title) return { before: eventPhrase(described), object: "", after: "" };
  if (verb.includes("{object}")) {
    const [before, after] = verb.split("{object}");
    return { before, object: title, after };
  }
  return { before: `${verb} `, object: title, after: "" };
}

function countPhrase(entry) {
  if (entry.count === 1) return eventPhrase(entry.sample);
  return entry.sample.plural.replace("{n}", String(entry.count));
}

function joinClauses(clauses) {
  if (clauses.length <= 1) return clauses.join("");
  return `${clauses.slice(0, -1).join(", ")} and ${clauses[clauses.length - 1]}`;
}

/**
 * The digest line for one update group: who did what, newest actor first.
 *
 * @param {Array<object>} events newest first, as `home.unread` returns them
 * @param {{ actorName?: (id: string) => string, titleFor?: (ref: string) => string,
 *   maxActors?: number, unreadCount?: number, selfId?: string }} [options]
 *   `selfId` is the reader, who reads as "You".
 * @returns {string} e.g. "Leo moved 2 tasks to review · Nina commented"
 */
export function updateDigest(events, options = {}) {
  const {
    actorName = (id) => id,
    titleFor = () => "",
    maxActors = 3,
    unreadCount = 0,
    selfId = "",
  } = options;
  const list = Array.isArray(events) ? events : [];
  /** @type {Map<string, Map<string, { count: number, sample: any }>>} */
  const byActor = new Map();
  for (const event of list) {
    const described = describeUpdateEvent(event, { titleFor });
    const actor = described.actorId || "";
    if (!byActor.has(actor)) byActor.set(actor, new Map());
    const verbs = byActor.get(actor);
    const entry = verbs.get(described.key);
    if (entry) {
      const identity =
        described.objectRef || text(event?.id) || String(entry.count);
      if (!entry.objects.has(identity)) {
        entry.objects.add(identity);
        entry.count += 1;
      }
    } else
      verbs.set(described.key, {
        count: 1,
        sample: described,
        objects: new Set([described.objectRef || text(event?.id)]),
      });
  }
  const important = (entry) =>
    entry.sample.key.startsWith("ask:") ||
    [
      "answered",
      "card_resolved",
      "card_moved:done",
      "card_moved:blocked",
    ].includes(entry.sample.key);
  const actorHasImportant = ([, verbs]) => [...verbs.values()].some(important);
  // Keep every material event and every actor who produced one. Routine edits
  // alone are capped; neither clause nor actor limits hide asks or transitions.
  const actors = [...byActor].sort(
    (a, b) => Number(actorHasImportant(b)) - Number(actorHasImportant(a)),
  );
  const parts = [];
  for (const [actorId, verbs] of actors) {
    const entries = [...verbs.values()];
    const material = entries.filter(important);
    if (parts.length >= maxActors && !material.length) continue;
    const who =
      selfId && actorId === selfId
        ? "You"
        : shortActorName(actorName(actorId)) || "Someone";
    const routine = entries.filter(
      (entry) =>
        entry.sample.key === "card:updated" ||
        entry.sample.key === "card_created",
    );
    const cards = new Set(routine.flatMap((entry) => [...entry.objects]));
    let clauses;
    if (
      options.isAgent?.(actorId) === true &&
      cards.size > 1 &&
      routine.length === verbs.size
    ) {
      clauses = [
        `reorganized ${titleFor(options.groupRef) || "initiatives"}: ${cards.size} cards`,
      ];
    } else {
      const ordinary = entries.filter((entry) => !important(entry));
      clauses = [...material, ...ordinary.slice(0, 2)].map(countPhrase);
      if (ordinary.length > 2)
        clauses.push(`${ordinary.length - 2} more routine changes`);
    }
    parts.push(`${who} ${joinClauses(clauses)}`);
  }
  const hiddenActors = byActor.size - parts.length;
  if (hiddenActors > 0) {
    parts.push(`${hiddenActors} ${hiddenActors === 1 ? "other" : "others"}`);
  }
  if (!parts.length) {
    return unreadCount > 0
      ? `${unreadCount} new ${unreadCount === 1 ? "change" : "changes"}`
      : "";
  }
  return parts.join(" · ");
}
