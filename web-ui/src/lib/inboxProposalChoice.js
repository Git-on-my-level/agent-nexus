/**
 * Choosing a suggested response.
 *
 * Two rules, and both exist because sending is irreversible-looking even when
 * it is not: nothing is highlighted until the reader acts — not even the
 * requester's recommendation, which only wears a badge — and a number key
 * selects before it sends.
 *
 * A click is already a deliberate act on a specific option, so it sends. A
 * number key is one keystroke away from the key beside it, so the first press
 * highlights, the same key again sends, a different number moves the
 * highlight, and Escape clears it.
 */

/** How many suggestions get a number key. */
export const MAX_KEYED_PROPOSALS = 5;

/**
 * How long the chosen option flashes before the response goes out.
 *
 * Short enough to read as feedback rather than a wait, and well under the
 * 500 ms the spec allows. Reduced motion skips the animation and the delay.
 */
export const PROPOSAL_FLASH_MS = 220;

/**
 * What a number key means right now.
 *
 * @param {{ index: number, armed: number, count: number }} input
 *   `index` is zero-based, `armed` is the highlighted index or -1.
 * @returns {"arm" | "send" | "ignore"}
 */
export function proposalKeyAction({ index, armed, count }) {
  const total = Number(count) || 0;
  const wanted = Number(index);
  if (!Number.isInteger(wanted) || wanted < 0 || wanted >= total)
    return "ignore";
  return wanted === armed ? "send" : "arm";
}

/** True when the reader asked for less motion, so the flash is skipped. */
export function prefersReducedMotion() {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return Boolean(window.matchMedia("(prefers-reduced-motion: reduce)").matches);
}
