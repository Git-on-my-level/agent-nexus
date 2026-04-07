/** Attribute on a <form> or field control to disable ⌘/Ctrl+Enter submission. */
export const FORM_SUBMIT_SHORTCUT_OPT_OUT = "data-oar-no-submit-shortcut";

/**
 * ⌘/Ctrl+Enter on a text-like control inside a <form> runs the form's implicit submit
 * (same as activating the default submit button), including constraint validation.
 *
 * @param {KeyboardEvent} event
 * @param {{ commandPaletteOpen?: boolean }} [options]
 * @returns {boolean} true if the event was handled (caller may want to stop further handling)
 */
export function handleModEnterFormSubmit(event, options = {}) {
  const { commandPaletteOpen = false } = options;
  if (commandPaletteOpen) return false;
  if (event.defaultPrevented || event.repeat) return false;
  if (event.key !== "Enter" || !(event.metaKey || event.ctrlKey)) return false;

  const target = event.target;
  if (!(target instanceof Element)) return false;
  if (target.closest(`[${FORM_SUBMIT_SHORTCUT_OPT_OUT}]`)) return false;
  if (!isTextLikeFormControl(target)) return false;

  const form = target.closest("form");
  if (!form || !form.isConnected) return false;
  if (form.hasAttribute(FORM_SUBMIT_SHORTCUT_OPT_OUT)) return false;

  try {
    form.requestSubmit();
  } catch {
    return false;
  }
  event.preventDefault();
  return true;
}

/**
 * @param {Element} el
 * @returns {boolean}
 */
function isTextLikeFormControl(el) {
  if (!(el instanceof HTMLElement)) return false;
  if (el.isContentEditable) return false;
  const tag = el.tagName;
  if (tag === "TEXTAREA") return true;
  if (tag === "SELECT") return true;
  if (tag !== "INPUT") return false;
  const type = (el.getAttribute("type") || "text").toLowerCase();
  const excluded = new Set([
    "button",
    "submit",
    "reset",
    "checkbox",
    "radio",
    "file",
    "hidden",
    "image",
    "range",
    "color",
  ]);
  return !excluded.has(type);
}
