/** @type {{ id: string, close: () => void } | null} */
let active = null;

/**
 * Ensures only one floating context menu is open (new one closes the previous).
 * @param {string} id
 * @param {() => void} close
 */
export function registerContextMenu(id, close) {
  if (active && active.id !== id) {
    active.close();
  }
  active = { id, close };
}

/** @param {string} id */
export function clearContextMenu(id) {
  if (active?.id === id) {
    active = null;
  }
}
