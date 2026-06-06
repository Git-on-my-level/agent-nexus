/**
 * Platform-aware keyboard shortcut labels for UI affordances.
 * Behavior handlers use `(event.metaKey || event.ctrlKey)`; this module is
 * display-only.
 */

/**
 * @returns {boolean}
 */
export function isMacPlatform() {
  if (typeof navigator === "undefined") return false;
  return /Mac|iP(hone|ad|od)/.test(navigator.platform ?? "");
}

/**
 * @returns {"⌘" | "Ctrl"}
 */
export function modSymbol() {
  return isMacPlatform() ? "⌘" : "Ctrl";
}

/**
 * Format a modifier shortcut for tooltips and aria labels.
 *
 * @param {string} key - Single key or special label (e.g. "S", "Enter", "⏎", "Esc")
 * @param {{ shift?: boolean, alt?: boolean }} [options]
 * @returns {string}
 */
export function formatShortcut(key, options = {}) {
  const { shift = false, alt = false } = options;
  const mac = isMacPlatform();
  const parts = [];
  if (mac) {
    if (shift) parts.push("⇧");
    if (alt) parts.push("⌥");
    parts.push("⌘");
    parts.push(key);
    return parts.join("");
  }
  if (shift) parts.push("Shift");
  if (alt) parts.push("Alt");
  parts.push("Ctrl");
  parts.push(key);
  return parts.join("+");
}
