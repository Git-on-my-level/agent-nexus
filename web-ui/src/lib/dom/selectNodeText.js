/**
 * Select all text in `el` (for double-click + copy workflows).
 * @param {HTMLElement} el
 */
export function selectNodeText(el) {
  if (!el) return;
  const range = document.createRange();
  range.selectNodeContents(el);
  const sel = getSelection();
  if (!sel) return;
  sel.removeAllRanges();
  sel.addRange(range);
}
