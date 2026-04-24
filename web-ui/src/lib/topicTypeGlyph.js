/** @typedef {"initiative"|"objective"|"decision"|"incident"|"risk"|"request"|"note"|"other"} TopicGlyphKind */

/**
 * Canonical 8 topic kinds surfaced in the UI (matches the simplification strategy §8 Q3
 * decision). The contract enum (`enums.topic_type`) still permits the legacy values
 * `case`, `process`, `relationship` for forward compatibility, but they are never
 * exposed as choices in the UI; existing topics carrying them are surfaced as
 * `other` by `normalizeTopicType` and an additional read-only "(legacy)" option in
 * the edit form (so users can change away without losing data).
 */
export const CANONICAL_TOPIC_TYPES = Object.freeze([
  "initiative",
  "objective",
  "decision",
  "incident",
  "risk",
  "request",
  "note",
  "other",
]);

const TOPIC_GLYPH_TYPES = new Set(CANONICAL_TOPIC_TYPES);

/** Human-readable label for a canonical topic type (used by select options). */
export const CANONICAL_TOPIC_TYPE_LABELS = Object.freeze({
  initiative: "Initiative",
  objective: "Objective",
  decision: "Decision",
  incident: "Incident",
  risk: "Risk",
  request: "Request",
  note: "Note",
  other: "Other",
});

/**
 * Existing topics created against the legacy `topic_type` enum may still carry
 * `case`, `process`, or `relationship`. Surface the current value as a legacy
 * option so the edit form mirrors what's saved without coercing on open.
 *
 * @param {unknown} currentType
 * @returns {{ value: string, label: string }[]}
 */
export function topicTypeSelectOptions(currentType = "") {
  const opts = CANONICAL_TOPIC_TYPES.map((value) => ({
    value,
    label: CANONICAL_TOPIC_TYPE_LABELS[value] ?? value,
  }));
  const current = String(currentType ?? "")
    .trim()
    .toLowerCase();
  if (current && !CANONICAL_TOPIC_TYPES.includes(current)) {
    opts.push({
      value: current,
      label: `${current.charAt(0).toUpperCase() + current.slice(1)} (legacy)`,
    });
  }
  return opts;
}

/**
 * @param {unknown} type
 * @returns {TopicGlyphKind}
 */
export function normalizeTopicType(type) {
  const t = String(type ?? "")
    .trim()
    .toLowerCase();
  if (TOPIC_GLYPH_TYPES.has(t)) return /** @type {TopicGlyphKind} */ (t);
  return "other";
}

/**
 * CSS variable name (without `var()`) for stroke/fill color, e.g. `--danger`.
 * @param {unknown} type
 * @returns {string}
 */
export function topicTypeColorVarName(type) {
  const t = normalizeTopicType(type);
  switch (t) {
    case "initiative":
      return "--ok";
    case "objective":
      return "--accent";
    case "decision":
      return "--accent";
    case "incident":
      return "--danger";
    case "risk":
      return "--warn";
    case "request":
      // Per polish §P9: avoid `--accent-text` here because it collides with
      // the theme accent already used by Decision/Objective. `--info` is the
      // dedicated neutral-informational token in the design system and keeps
      // Request distinct from Note/Other (which use the muted/subtle scale).
      return "--info";
    case "note":
      return "--fg-muted";
    case "other":
    default:
      return "--fg-subtle";
  }
}
