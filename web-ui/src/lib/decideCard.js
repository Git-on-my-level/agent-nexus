/** Small pure helpers for `DecisionCard.svelte`, which is at its size limit. */

/** True when the event target is a field the reader is typing in. */
export function editable(target) {
  return (
    target instanceof HTMLElement &&
    (target.isContentEditable ||
      ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
  );
}

/** No matchMedia (a test DOM) also counts as reduced motion. */
export function reducedMotion() {
  if (typeof window === "undefined" || !window.matchMedia) return true;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/** The outcome a proposal records; only a structured request can be refused. */
export function outcomeFor(structured, proposal) {
  if (!structured) return "answered";
  return /^(deny|reject|decline|refuse)/i.test(proposal)
    ? "rejected"
    : "approved";
}
