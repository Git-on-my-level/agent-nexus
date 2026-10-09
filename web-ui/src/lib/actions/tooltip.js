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

/**
 * How long a finger must stay down before the tip pins. A shorter tap pins
 * it too: touch has no hover, and a native `title` does not appear on mobile.
 */
export const TOOLTIP_HOLD_MS = 450;

export const activeTooltip = writable(
  /** @type {null | { text: string, rect: DOMRect }} */ (null),
);

let timer = null;
/** The node the pointer is on, so a stale timer cannot open the wrong tip. */
let armed = null;
/** A touch tip stays up after the finger lifts, until the next tap or scroll. */
let pinned = null;
/** Document listeners that exist only while a tip is pinned. */
let releasePinnedWatch = null;
/** Removes the one-shot listener that swallows the click after a reveal tap. */
let releaseClickSwallow = null;

function cancel() {
  if (timer) clearTimeout(timer);
  timer = null;
  armed = null;
}

function clearPinnedWatch() {
  releasePinnedWatch?.();
  releasePinnedWatch = null;
}

function hide(node, force = false) {
  if (!force && pinned === node) return;
  if (node && armed && armed !== node) return;
  if (!force && pinned && pinned !== node) return;
  pinned = null;
  clearPinnedWatch();
  cancel();
  activeTooltip.set(null);
}

/**
 * While a touch tip is pinned, an outside tap or Escape dismisses it.
 * The listeners exist only for that pin and do not cancel the outside tap,
 * so the button the reader actually hit still runs.
 */
function watchPinned(node) {
  clearPinnedWatch();
  const onOutside = (event) => {
    if (pinned !== node) return;
    const target = event.target;
    if (target instanceof Node && node.contains(target)) return;
    hide(node, true);
  };
  const onEscape = (event) => {
    if (event.key !== "Escape" || pinned !== node) return;
    hide(node, true);
  };
  document.addEventListener("pointerdown", onOutside, true);
  document.addEventListener("keydown", onEscape, true);
  releasePinnedWatch = () => {
    document.removeEventListener("pointerdown", onOutside, true);
    document.removeEventListener("keydown", onEscape, true);
  };
}

/**
 * Chromium still synthesizes a click after a touch whose pointerdown was
 * cancelled. Swallow that one click, and only that one, so a timestamp
 * inside a link does not navigate and the rest of the link still does.
 */
function swallowFollowingClick() {
  releaseClickSwallow?.();
  const stop = (event) => {
    event.preventDefault();
    event.stopPropagation();
    release();
  };
  const release = () => {
    document.removeEventListener("click", stop, true);
    document.removeEventListener("pointerdown", release, true);
    if (releaseClickSwallow === release) releaseClickSwallow = null;
  };
  document.addEventListener("click", stop, true);
  document.addEventListener("pointerdown", release, true);
  releaseClickSwallow = release;
}

function insideForeignControl(node) {
  const control = node.closest("a, button");
  return Boolean(control) && control !== node;
}

function pin(node, text) {
  const body = String(text ?? "").trim();
  if (!body) return;
  if (timer) clearTimeout(timer);
  timer = null;
  armed = node;
  pinned = node;
  activeTooltip.set({ text: body, rect: node.getBoundingClientRect() });
  watchPinned(node);
}

function show(node, text, delay) {
  const body = String(text ?? "").trim();
  if (!body) return;
  cancel();
  pinned = null;
  clearPinnedWatch();
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

  let hold = null;
  let pressed = false;
  let revealTouch = false;
  const clearHold = () => {
    if (hold) clearTimeout(hold);
    hold = null;
  };
  const onEnter = (event) => {
    // A touch sends pointerenter before pointerdown. Showing here would
    // clear a pin before the second tap can dismiss it.
    if (event.pointerType === "touch" || event.pointerType === "pen") return;
    show(node, current, TOOLTIP_DELAY_MS);
  };
  const onFocus = () => show(node, current, 0);
  const onLeave = () => {
    // Touch can emit pointerleave before pointerup. Cancelling there would
    // swallow the tap that is supposed to reveal the exact time.
    if (pressed) return;
    clearHold();
    hide(node);
  };
  const onCancel = () => {
    pressed = false;
    revealTouch = false;
    clearHold();
    if (pinned !== node) hide(node);
  };
  const onKey = (event) => {
    if (event.key === "Escape") hide(node, true);
  };
  const onDown = (event) => {
    clearHold();
    const touch = event.pointerType === "touch" || event.pointerType === "pen";
    if (!touch || !current) {
      pressed = false;
      revealTouch = false;
      if (!touch) hide(node, true);
      return;
    }
    // A tip on a link or button must still activate that control. A tip
    // inside one, such as a timestamp in a conversation row, must not.
    if (insideForeignControl(node)) event.preventDefault();
    event.stopPropagation();
    // A second tap dismisses the sentence the first tap pinned.
    if (pinned === node) {
      pressed = false;
      revealTouch = false;
      hide(node, true);
      // Closing the tip is not a request to follow the link around it.
      if (insideForeignControl(node)) swallowFollowingClick();
      return;
    }
    if (pinned) hide(pinned, true);
    pressed = true;
    revealTouch = true;
    hold = setTimeout(() => {
      hold = null;
      pressed = false;
      pin(node, current);
    }, TOOLTIP_HOLD_MS);
  };
  const onUp = (event) => {
    const touch = event.pointerType === "touch" || event.pointerType === "pen";
    if (!touch) return;
    if (revealTouch && insideForeignControl(node)) swallowFollowingClick();
    revealTouch = false;
    if (pressed || hold || pinned === node) {
      event.preventDefault();
      event.stopPropagation();
    }
    if (!pressed) return;
    pressed = false;
    if (!hold || pinned === node) return;
    clearHold();
    pin(node, current);
  };

  node.addEventListener("pointerenter", onEnter);
  node.addEventListener("pointerleave", onLeave);
  node.addEventListener("pointercancel", onCancel);
  node.addEventListener("pointerdown", onDown);
  node.addEventListener("pointerup", onUp);
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
      clearHold();
      hide(node, true);
      node.removeEventListener("pointerenter", onEnter);
      node.removeEventListener("pointerleave", onLeave);
      node.removeEventListener("pointercancel", onCancel);
      node.removeEventListener("pointerdown", onDown);
      node.removeEventListener("pointerup", onUp);
      node.removeEventListener("focusin", onFocus);
      node.removeEventListener("focusout", onLeave);
      node.removeEventListener("keydown", onKey);
    },
  };
}

/** Close whatever is open (a navigation, a scroll, a dialog opening). */
export function hideTooltip() {
  pinned = null;
  clearPinnedWatch();
  // Leave the one-shot click swallow in place. Scroll calls this between
  // pointerup and the click Chromium still emits, and releasing here would
  // let that click follow the link. The swallow removes itself on that
  // click, or on the next pointerdown if the click never comes.
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
