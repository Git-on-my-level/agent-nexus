import { isTextEntryTarget } from "$lib/inboxShortcuts.js";

/**
 * Agents roster keys. Single letters never fire while typing, with a
 * modifier other than Shift held, or while another dialog is open.
 */
export function agentShortcutList() {
  return [
    ["Next agent", ["J"]],
    ["Previous agent", ["K"]],
    ["Open the agent", ["Enter"]],
    ["Answer its ask in Inbox", ["I"]],
    ["Open its current task", ["T"]],
    ["Shortcut help", ["?"]],
    ["Close this help", ["Esc"]],
  ];
}

/**
 * @param {KeyboardEvent} event
 * @param {{ helpOpen?: boolean, modalOpen?: boolean }} [state]
 * @returns {null | { type: "next" | "previous" | "open" | "inbox" | "task" | "help" | "close-help" }}
 */
export function agentShortcutAction(
  event,
  { helpOpen = false, modalOpen = false } = {},
) {
  if (!event || event.defaultPrevented || event.isComposing) return null;
  if (event.key === "Escape") return helpOpen ? { type: "close-help" } : null;
  if (event.altKey || event.metaKey || event.ctrlKey) return null;
  if (isTextEntryTarget(event.target)) return null;
  if (event.key === "?")
    return helpOpen ? { type: "close-help" } : { type: "help" };
  if (helpOpen || modalOpen) return null;
  switch (event.key) {
    case "j":
    case "ArrowDown":
      return { type: "next" };
    case "k":
    case "ArrowUp":
      return { type: "previous" };
    case "Enter":
    case "o":
      // Enter on a focused link or button is that control's own action.
      if (
        event.key === "Enter" &&
        typeof event.target?.closest === "function" &&
        event.target.closest("a, button")
      )
        return null;
      return { type: "open" };
    case "i":
      return { type: "inbox" };
    case "t":
      return { type: "task" };
    default:
      return null;
  }
}
