/**
 * What a Threads row says besides its title.
 *
 * Thread titles are inherited from whatever a thread backs, and core mints
 * some of them from a fixed word: every per-actor ask board backs a thread
 * called "Asks". A list of titles alone then has two identical rows and no
 * way to tell which is which.
 *
 * Both answers are already on the row: `subject_ref` says what kind of thing
 * the thread belongs to, and `updated_by` says whose it is — a name the shell
 * has already loaded. Neither costs a request.
 */

import { parseRef } from "./typedRefs.js";

/** Operator nouns, matching the ref chips: `topic:` is a Project, `card:` a Task. */
const SUBJECT_LABELS = Object.freeze({
  board: "Group",
  card: "Initiative",
  doc: "Doc",
  document: "Doc",
  thread: "Thread",
  topic: "Project",
});

/** The subject ref a thread row carries (`topic_ref` on older payloads). */
export function threadSubjectRef(thread) {
  return String(thread?.subject_ref ?? thread?.topic_ref ?? "").trim();
}

/** "Board", "Task", … for a thread's subject, or "" when it has none. */
export function threadSubjectKindLabel(thread) {
  const { prefix } = parseRef(threadSubjectRef(thread));
  return SUBJECT_LABELS[String(prefix ?? "").toLowerCase()] ?? "";
}

/**
 * The context parts for a row, in reading order, with the empties dropped.
 *
 * @param {unknown} thread
 * @param {{ ownerName?: string }} [options]
 * @returns {string[]}
 */
export function threadRowContextParts(thread, { ownerName = "" } = {}) {
  return [
    threadSubjectKindLabel(thread),
    String(ownerName ?? "").trim(),
  ].filter(Boolean);
}
