/** Attribute on a <form> or field control to disable ⌘/Ctrl+Enter submission. */
export const FORM_SUBMIT_SHORTCUT_OPT_OUT = "data-anx-no-submit-shortcut";

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
  if (event.repeat) return false;
  if (event.key !== "Enter" || !(event.metaKey || event.ctrlKey)) return false;

  const fromEvent = event.target instanceof Element ? event.target : null;
  const fromActive =
    document.activeElement instanceof HTMLElement &&
    isTextLikeFormControl(document.activeElement)
      ? document.activeElement
      : null;
  const target =
    fromEvent && isTextLikeFormControl(fromEvent) ? fromEvent : fromActive;
  if (!target) return false;
  if (target.closest(`[${FORM_SUBMIT_SHORTCUT_OPT_OUT}]`)) return false;
  if (!isTextLikeFormControl(target)) return false;

  const form = target.closest("form");
  if (!form || !form.isConnected) return false;
  if (form.hasAttribute(FORM_SUBMIT_SHORTCUT_OPT_OUT)) return false;

  const submitted = requestFormSubmit(form);
  if (!submitted) return false;
  event.preventDefault();
  return true;
}

/**
 * Prefer {@link HTMLFormElement#requestSubmit}; fall back so SPA `onsubmit` handlers
 * still run if the browser throws or skips firing (e.g. edge cases around submitters).
 * @param {HTMLFormElement} form
 * @returns {boolean}
 */
function requestFormSubmit(form) {
  try {
    form.requestSubmit();
    return true;
  } catch {
    const enabledSubmitter = form.querySelector(
      'button[type="submit"]:not([disabled]), input[type="submit"]:not([disabled])',
    );
    if (enabledSubmitter) {
      try {
        form.requestSubmit(enabledSubmitter);
        return true;
      } catch {
        /* fall through */
      }
    }
    try {
      const ev = new SubmitEvent("submit", { bubbles: true, cancelable: true });
      return form.dispatchEvent(ev);
    } catch {
      return form.dispatchEvent(
        new Event("submit", { bubbles: true, cancelable: true }),
      );
    }
  }
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
