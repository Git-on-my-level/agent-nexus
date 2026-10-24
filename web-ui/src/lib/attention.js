/**
 * Attention: where ANX's agents placed an item for the reader, and the
 * reader's feedback on that placement. ANX runs no models; it stores the
 * placements agents write and the feedback readers give, and agents read the
 * feedback to learn. These are the signals core accepts.
 */

/** Feedback choices per surface: `[signal, label, hint]`. */
export const FEEDBACK_CHOICES = {
  decide: [
    ["didnt_need_me", "Didn't need me", "An agent could have decided this"],
    [
      "needed_sooner",
      "Needed me sooner",
      "This should have reached me earlier",
    ],
    ["not_enough_context", "Not enough context", "I can't decide from this"],
    ["wrong_person", "Wrong person", "Someone else should decide"],
    ["should_be_watch", "Just tell me in Watch", "Worth knowing, not deciding"],
  ],
  watch: [
    ["more_like_this", "Show more like this", ""],
    ["less_like_this", "Show less like this", ""],
    ["not_relevant", "Not relevant to me", ""],
    ["should_be_decide", "Should have been in Decide", "It needed an answer"],
    ["already_knew", "Already knew this", ""],
  ],
  all: [
    ["should_be_decide", "Should have been in Decide", ""],
    ["should_be_watch", "Should have been in Watch", ""],
    ["wanted_to_see", "I wanted to see this", "It was hidden as routine"],
    ["less_like_this", "Show less like this", ""],
    ["not_relevant", "Not relevant to me", ""],
  ],
};

export const FEEDBACK_SIGNALS = new Set(
  Object.values(FEEDBACK_CHOICES)
    .flat()
    .map(([signal]) => signal),
);

export const SIGNAL_LABELS = Object.fromEntries(
  Object.values(FEEDBACK_CHOICES)
    .flat()
    .map(([signal, label]) => [signal, label]),
);

export const SURFACE_LABELS = {
  decide: "Decide",
  watch: "Watch",
  digest: "Digest",
  routine: "Routine",
};

/** Who placed an item, in words. */
export function placedByLabel(placedBy) {
  const value = String(placedBy ?? "").trim();
  if (!value || value === "anx_default") return "ANX default";
  if (value === "anx_guarantee") return "ANX guarantee";
  return value.replace(/^(agent|actor|human):/, "");
}
