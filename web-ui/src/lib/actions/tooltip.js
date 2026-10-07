import { writable } from "svelte/store";

/**
 * Tooltips that appear when you point at the thing.
 *
 * The browser's own `title` tooltip waits about a second before it shows and
 * paints a question-mark cursor while you wait for it, which turns "what is
 * this number?" into a pause and a guess. Neither delay nor cursor is
 * configurable, so a tooltip that has to be quick cannot be a `title`.
 *
 * This is the replacement: one floating layer per page (`TooltipHost`), fed by
 * an action that reports which element is pointed at and what it says. The
 * delay is {@link TOOLTIP_DELAY_MS} — long enough that sweeping the pointer
 * across a row of badges does not strobe, short enough to read as instant.
 *
 * The trigger keeps its own accessible name (`aria-label`), so the tooltip
 * layer itself is `aria-hidden`: a screen reader already has the sentence and
 * does not need it announced twice.
 */

/** Small enough to read as instant, large enough not to strobe on a sweep. */
export const TOOLTIP_DELAY_MS = 60;

export const activeTooltip = writable(
  /** @type {null | { text: string, rect: DOMRect }} */ (null),
);

let timer = null;
/** The node the pointer is on, so a stale timer cannot open the wrong tip. */
let armed = null;

function cancel() {
  if (timer) clearTimeout(timer);
  timer = null;
  armed = null;
}

function hide(node) {
  if (armed && armed !== node) return;
  cancel();
  activeTooltip.set(null);
}

function show(node, text, delay) {
  const body = String(text ?? "").trim();
  if (!body) return;
  cancel();
  armed = node;
  const open = () => {
    timer = null;
    if (armed !== node || !node.isConnected) return;
    activeTooltip.set({ text: body, rect: node.getBoundingClientRect() });
  };
  if (delay <= 0) open();
  else timer = setTimeout(open, delay);
}

/**
 * `use:tooltip={"Moved Oct 5, 2026, 11:12 (8h)"}` — the sentence behind a
 * compact badge, on hover and on keyboard focus.
 *
 * Focus opens with no delay: a reader who tabbed to the control has already
 * declared intent, and a hover delay there only feels sluggish.
 *
 * The text is also written to `data-tooltip`, which is what a test asserts
 * against now that there is no `title` to read.
 *
 * @param {HTMLElement} node
 * @param {string} text
 */
export function tooltip(node, text) {
  let current = String(text ?? "").trim();

  const apply = () => {
    if (current) node.setAttribute("data-tooltip", current);
    else node.removeAttribute("data-tooltip");
  };
  apply();

  const onEnter = () => show(node, current, TOOLTIP_DELAY_MS);
  const onFocus = () => show(node, current, 0);
  const onLeave = () => hide(node);
  const onKey = (event) => {
    if (event.key === "Escape") hide(node);
  };

  node.addEventListener("pointerenter", onEnter);
  node.addEventListener("pointerleave", onLeave);
  // A tap should not leave a tooltip stranded over what it was pointing at.
  node.addEventListener("pointercancel", onLeave);
  node.addEventListener("pointerdown", onLeave);
  node.addEventListener("focusin", onFocus);
  node.addEventListener("focusout", onLeave);
  node.addEventListener("keydown", onKey);

  return {
    update(next) {
      current = String(next ?? "").trim();
      apply();
      // Retarget an open tooltip rather than leaving yesterday's sentence up.
      activeTooltip.update((open) =>
        armed === node && open
          ? current
            ? { text: current, rect: node.getBoundingClientRect() }
            : null
          : open,
      );
    },
    destroy() {
      hide(node);
      node.removeEventListener("pointerenter", onEnter);
      node.removeEventListener("pointerleave", onLeave);
      node.removeEventListener("pointercancel", onLeave);
      node.removeEventListener("pointerdown", onLeave);
      node.removeEventListener("focusin", onFocus);
      node.removeEventListener("focusout", onLeave);
      node.removeEventListener("keydown", onKey);
    },
  };
}

/** Close whatever is open (a navigation, a scroll, a dialog opening). */
export function hideTooltip() {
  cancel();
  activeTooltip.set(null);
}

/**
 * Where the layer goes: above the trigger when there is room, below it when
 * there is not, and never off the side of the window.
 *
 * @param {DOMRect} rect the trigger's box
 * @param {{ width: number, height: number }} size the tooltip's own box
 * @param {{ width: number, height: number }} viewport
 * @param {number} [gap]
 */
export function tooltipPosition(rect, size, viewport, gap = 6) {
  const margin = 8;
  const above = rect.top - size.height - gap;
  const below = rect.bottom + gap;
  const placement = above >= margin ? "top" : "bottom";
  const top =
    placement === "top"
      ? above
      : Math.min(
          below,
          Math.max(margin, viewport.height - size.height - margin),
        );
  const centred = rect.left + rect.width / 2 - size.width / 2;
  const left = Math.min(
    Math.max(margin, centred),
    Math.max(margin, viewport.width - size.width - margin),
  );
  return { top: Math.round(top), left: Math.round(left), placement };
}
