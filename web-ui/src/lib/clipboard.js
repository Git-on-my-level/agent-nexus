/**
 * Write text to the clipboard. Returns true on success, false if the
 * clipboard API is unavailable or the write was rejected.
 *
 * @param {unknown} value
 * @returns {Promise<boolean>}
 */
export async function copyText(value) {
  try {
    await navigator.clipboard.writeText(String(value ?? ""));
    return true;
  } catch {
    return false;
  }
}
