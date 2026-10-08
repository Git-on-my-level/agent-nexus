/**
 * The plan graph's own health token.
 *
 * Not a card's status — `workSummary.js` owns that, and every surface renders
 * it through `WorkSummary.svelte`. This reads only the plan state's health,
 * which `planShape.js` uses to colour the plan tree, and keeps the two
 * spellings core has used for it.
 */
export function planStateHealth(planState) {
  const state = planState?.health_state ?? planState?.health;
  if (typeof state !== "string") return "";
  return state === "stalled" ? "stale" : state;
}
