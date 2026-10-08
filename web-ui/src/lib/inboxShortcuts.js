import { modSymbol } from "$lib/keyboardHints.js";

/**
 * Inbox triage keys. Both Inbox surfaces (the pane and the standalone item
 * page) map keys through here, so a key means the same thing everywhere.
 *
 * Single-letter keys never fire while the reader is typing, while a modifier
 * other than Shift is held, or while another dialog is open.
 */

/** Rows for the `?` help dialog, with the platform's modifier. */
export function inboxShortcutList() {
  const mod = modSymbol();
  return [
    ["Next item", ["J"]],
    ["Previous item", ["K"]],
    ["Select a suggested response, press again to send", ["1", "–", "5"]],
    ["Write a reply", ["R"]],
    ["Send the reply", [mod, "Enter"]],
    ["Acknowledge, or mark read", ["E"]],
    ["Open the task or doc", ["O"]],
    ["Undo the last response", [mod, "Z"]],
    ["Shortcut help", ["?"]],
    ["Clear the selection, or close this help", ["Esc"]],
  ];
}

export function isTextEntryTarget(target) {
  if (!target || typeof target !== "object") return false;
  const element = /** @type {HTMLElement} */ (target);
  if (element.isContentEditable) return true;
  const tag = String(element.tagName ?? "").toUpperCase();
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  if (tag !== "INPUT") return false;
  const type = String(element.getAttribute?.("type") ?? "text").toLowerCase();
  return ![
    "button",
    "checkbox",
    "radio",
    "submit",
    "reset",
    "file",
    "range",
    "color",
  ].includes(type);
}

/**
 * The triage action a keydown asks for, or null.
 *
 * @param {KeyboardEvent} event
 * @param {{ helpOpen?: boolean, modalOpen?: boolean }} [state]
 * @returns {null | { type: "next" | "previous" | "proposal" | "reply" | "done"
 *   | "open" | "undo" | "help" | "close-help" | "clear-choice", index?: number }}
 */
export function inboxShortcutAction(
  event,
  { helpOpen = false, modalOpen = false } = {},
) {
  if (!event || event.defaultPrevented || event.isComposing) return null;
  /*
   * Escape clears a highlighted suggestion. Two things keep it from stealing
   * the key: a dialog that is open owns Escape outright, and the caller
   * decides whether a highlight was actually there — an Escape that cleared
   * nothing must keep falling through to whatever else listens for it (a
   * popover, a draft-discarding blur).
   */
  if (event.key === "Escape") {
    if (helpOpen) return { type: "close-help" };
    return modalOpen ? null : { type: "clear-choice" };
  }
  if (event.altKey || isTextEntryTarget(event.target)) return null;
  const mod = event.metaKey || event.ctrlKey;
  if (mod) {
    if (
      !event.shiftKey &&
      String(event.key).toLowerCase() === "z" &&
      !modalOpen
    )
      return { type: "undo" };
    return null;
  }
  if (event.key === "?")
    return helpOpen ? { type: "close-help" } : { type: "help" };
  if (helpOpen || modalOpen) return null;
  if (event.key === "j") return { type: "next" };
  if (event.key === "k") return { type: "previous" };
  if (/^[1-5]$/.test(event.key)) {
    /*
     * A held key must not answer. The second press that sends has to be a
     * second press: auto-repeat would select on the first keydown and send
     * on the next one, half a second later, without the reader doing
     * anything. Repeats are fine for J/K, which only move.
     */
    if (event.repeat) return null;
    return { type: "proposal", index: Number(event.key) };
  }
  if (event.key === "r") return { type: "reply" };
  if (event.key === "e") return { type: "done" };
  if (event.key === "o") return { type: "open" };
  return null;
}

/** True while some other dialog (palette, confirm) owns the keyboard. */
export function otherDialogOpen(ownDialog = null) {
  if (typeof document === "undefined") return false;
  return [...document.querySelectorAll('[aria-modal="true"]')].some(
    (node) => node !== ownDialog,
  );
}
