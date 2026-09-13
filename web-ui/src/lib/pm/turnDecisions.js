const DECISION_REF_PATTERN = /\bdecision:[A-Za-z0-9._:-]+/g;

/**
 * Decision ids a PM turn points at: refs recorded as evidence plus
 * `decision:<id>` mentions in the reply text (covers turns completed before
 * the runner began recording decision refs as evidence).
 *
 * @param {{ evidence_refs?: string[], response?: string } | null} turn
 * @returns {string[]} unique decision ids in first-seen order
 */
export function decisionIdsFromTurn(turn) {
  // Core records the proposed ids on the turn; evidence refs and prose
  // mentions cover turns from runners that predate that field.
  const refs = [
    ...(Array.isArray(turn?.decision_ids)
      ? turn.decision_ids.map((id) => `decision:${id}`)
      : []),
    ...(turn?.evidence_refs || []),
  ];
  if (typeof turn?.response === "string") {
    refs.push(...(turn.response.match(DECISION_REF_PATTERN) || []));
  }
  const seen = new Set();
  const ids = [];
  for (const ref of refs) {
    if (!String(ref).startsWith("decision:")) continue;
    // Sentence punctuation trailing the match (`decision:x.`) is not part of
    // the id; interior separators stay.
    const id = String(ref)
      .slice("decision:".length)
      .replace(/[._:-]+$/, "");
    if (id && !seen.has(id)) {
      seen.add(id);
      ids.push(id);
    }
  }
  return ids;
}
