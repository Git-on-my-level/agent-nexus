/**
 * Component-owned Escape: revert draft then optional cleanup (e.g. exit edit mode).
 * Stops propagation by default so window-level Escape blur helpers do not run.
 *
 * @param {HTMLElement} node
 * @param {{
 *   onRevert?: (el: HTMLElement) => void,
 *   onAfter?: (el: HTMLElement) => void,
 *   disabled?: boolean,
 *   stopPropagation?: boolean
 * }} options
 */
export function inlineEditEscape(node, options) {
  let opts = {
    stopPropagation: true,
    ...options,
  };

  /** @param {KeyboardEvent} e */
  function onKeydown(e) {
    if (opts.disabled) return;
    if (e.key !== "Escape" || e.repeat) return;
    if (!opts.onRevert && !opts.onAfter) return;
    e.preventDefault();
    if (opts.stopPropagation) e.stopPropagation();
    opts.onRevert?.(node);
    opts.onAfter?.(node);
  }

  node.addEventListener("keydown", onKeydown);

  return {
    /**
     * @param {typeof options} next
     */
    update(next) {
      opts = { stopPropagation: true, ...next };
    },
    destroy() {
      node.removeEventListener("keydown", onKeydown);
    },
  };
}
