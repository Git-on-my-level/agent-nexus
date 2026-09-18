/**
 * Svelte action: keeps keyboard focus inside a modal layer.
 *
 * Apply it to the element that carries `role="dialog"` / `aria-modal="true"`.
 * Because modal layers in this app live inside `{#if open}`, the action's
 * lifetime is the dialog's lifetime: it moves focus in on mount, cycles Tab /
 * Shift+Tab inside the node, and puts focus back where it came from on
 * destroy. Escape handling stays with the component (`dismissOnEscape` or its
 * own keydown handler) — this action never closes anything.
 *
 * @typedef {Object} FocusTrapOptions
 * @property {boolean} [enabled] Trap only while true (default true).
 * @property {() => (HTMLElement | null | undefined)} [initialFocus]
 *   What to focus on open. Defaults to the first focusable descendant, then
 *   the node itself.
 * @property {boolean} [restoreFocus]
 *   Put focus back on the element that was focused before the dialog opened
 *   (default true).
 * @property {() => (HTMLElement | null | undefined)} [fallbackFocus]
 *   Where to send focus on close when the opener is gone (a palette opened by
 *   a shortcut with nothing focused would otherwise strand it on `<body>`).
 */

const FOCUSABLE_SELECTOR = [
  "a[href]",
  "area[href]",
  "button",
  "input",
  "select",
  "textarea",
  "summary",
  "iframe",
  "audio[controls]",
  "video[controls]",
  '[contenteditable="true"]',
  "[tabindex]",
].join(",");

/**
 * Tabbable descendants of `node`, in document order.
 *
 * jsdom has no layout, so the geometry check only runs when the node itself
 * measures — in a real browser an open dialog always does.
 *
 * @param {HTMLElement} node
 * @returns {HTMLElement[]}
 */
function tabbableWithin(node) {
  const measurable = node.getClientRects().length > 0;
  const view = node.ownerDocument?.defaultView ?? null;
  return Array.from(node.querySelectorAll(FOCUSABLE_SELECTOR)).filter((el) => {
    if (!(el instanceof HTMLElement)) return false;
    if (el.hasAttribute("disabled") || el.tabIndex < 0) return false;
    if (el.closest('[aria-hidden="true"], [hidden]')) return false;
    if (el instanceof HTMLInputElement && el.type === "hidden") return false;
    const style = view?.getComputedStyle?.(el);
    if (style && (style.display === "none" || style.visibility === "hidden")) {
      return false;
    }
    if (measurable && el.getClientRects().length === 0) return false;
    return true;
  });
}

/** @param {unknown} el */
function focusIfPossible(el) {
  if (el instanceof HTMLElement && el.isConnected) {
    el.focus();
    return true;
  }
  return false;
}

/**
 * @param {HTMLElement} node
 * @param {FocusTrapOptions} [options]
 */
export function focusTrap(node, options = {}) {
  /** @type {FocusTrapOptions} */
  let current = { ...options };
  /** @type {HTMLElement | null} */
  let opener = null;
  let trapping = false;

  const doc = node.ownerDocument ?? document;

  function activeElement() {
    const active = doc.activeElement;
    return active instanceof HTMLElement ? active : null;
  }

  /** @param {KeyboardEvent} event */
  function onKeydown(event) {
    if (!trapping || event.key !== "Tab" || event.defaultPrevented) return;
    const tabbable = tabbableWithin(node);
    if (tabbable.length === 0) {
      // Nothing to move to: hold focus on the dialog itself.
      event.preventDefault();
      focusIfPossible(node);
      return;
    }
    const first = tabbable[0];
    const last = tabbable[tabbable.length - 1];
    const active = activeElement();
    // `Node.contains` is inclusive, so focus on the trap node itself (e.g. a
    // click on non-tabbable chrome of a `tabindex="-1"` dialog) is not an
    // escape. Same for other non-tabbable descendants. Send those to the
    // first/last control instead of letting Tab walk into the page behind.
    if (!active || !tabbable.includes(active)) {
      event.preventDefault();
      focusIfPossible(event.shiftKey ? last : first);
      return;
    }
    if (!event.shiftKey && active === last) {
      event.preventDefault();
      focusIfPossible(first);
    } else if (event.shiftKey && active === first) {
      event.preventDefault();
      focusIfPossible(last);
    }
  }

  function activate() {
    if (trapping) return;
    trapping = true;
    // `<body>` is where focus lands when the dialog was opened by a shortcut;
    // an element inside the dialog means it already took focus. Neither is
    // worth restoring to, so both fall through to `fallbackFocus`.
    opener = activeElement();
    if (opener === doc.body || (opener && node.contains(opener))) opener = null;
    doc.addEventListener("keydown", onKeydown, true);
    const preferred = current.initialFocus?.();
    if (!focusIfPossible(preferred)) {
      if (!focusIfPossible(tabbableWithin(node)[0])) {
        if (!node.hasAttribute("tabindex")) node.setAttribute("tabindex", "-1");
        focusIfPossible(node);
      }
    }
  }

  function deactivate() {
    if (!trapping) return;
    trapping = false;
    doc.removeEventListener("keydown", onKeydown, true);
    if (current.restoreFocus === false) return;
    if (opener?.isConnected && opener !== doc.body) {
      opener.focus();
      opener = null;
      return;
    }
    opener = null;
    const fallback = current.fallbackFocus?.();
    // A landmark (`<main>`) has to be made programmatically focusable; a
    // button already is, and giving it tabindex="-1" would drop it out of the
    // tab order.
    if (
      fallback instanceof HTMLElement &&
      fallback.tabIndex < 0 &&
      !fallback.hasAttribute("tabindex")
    ) {
      fallback.setAttribute("tabindex", "-1");
    }
    focusIfPossible(fallback);
  }

  if (current.enabled !== false) activate();

  return {
    /** @param {FocusTrapOptions} [next] */
    update(next = {}) {
      current = { ...current, ...next };
      if (current.enabled === false) deactivate();
      else activate();
    },
    destroy() {
      deactivate();
    },
  };
}
