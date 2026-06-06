/**
 * Svelte action: while `enabled`, Escape on `document` (capture) calls
 * `onDismiss` and stops propagation so other global handlers do not run.
 *
 * @param {HTMLElement} _node
 * @param {{ onDismiss: () => void, enabled?: boolean }} [options]
 */
export function dismissOnEscape(_node, options = {}) {
  let current = {
    onDismiss: options.onDismiss ?? (() => {}),
    enabled: options.enabled !== false,
  };

  /** @param {KeyboardEvent} event */
  function onKeydown(event) {
    if (!current.enabled) return;
    if (event.key !== "Escape") return;
    if (event.defaultPrevented) return;
    event.preventDefault();
    event.stopPropagation();
    current.onDismiss();
  }

  document.addEventListener("keydown", onKeydown, true);

  return {
    /** @param {{ onDismiss?: () => void, enabled?: boolean }} next */
    update(next) {
      current = {
        onDismiss: next.onDismiss ?? current.onDismiss,
        enabled: next.enabled !== false,
      };
    },
    destroy() {
      document.removeEventListener("keydown", onKeydown, true);
    },
  };
}
