/** Attribute on a `<form>` or field control to disable ⌘/Ctrl+Enter submission. */
export const FORM_SUBMIT_SHORTCUT_OPT_OUT = "data-anx-no-submit-shortcut";

/** Opt out of blur-commit and Escape-blur text shortcuts (`handleModEnterBlurCommit`, `handleEscapeTextBlurCommit`). */
export const TEXT_SHORTCUT_OPT_OUT = "data-anx-no-text-shortcut";

/** Value `blur`: ⌘/Ctrl+Enter blurs the focused text control (commit via `onblur`). */
export const MOD_ENTER_COMMIT_ATTR = "data-anx-mod-enter-commit";

/** Value `blur`: Escape blurs the focused text control. Use sparingly. */
export const ESCAPE_DISMISS_ATTR = "data-anx-escape-dismiss";

/**
 * ⌘/Ctrl+Enter on a text-like control inside a `<form>` runs the form's implicit submit
 * (same as activating the default submit button), including constraint validation.
 *
 * @param {KeyboardEvent} event
 * @param {{ commandPaletteOpen?: boolean }} [options]
 * @returns {boolean} true if the event was handled (caller may want to stop further handling)
 */
export function handleModEnterFormSubmit(event, options = {}) {
  const { commandPaletteOpen = false } = options;
  if (commandPaletteOpen) return false;
  if (event.defaultPrevented) return false;
  if (event.repeat) return false;
  if (event.key !== "Enter" || !(event.metaKey || event.ctrlKey)) return false;

  const target = findFormSubmitTextTarget(event);
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
 * ⌘/Ctrl+Enter on a control tagged with `data-anx-mod-enter-commit="blur"` blurs it
 * so existing `onblur` commit paths run.
 *
 * @param {KeyboardEvent} event
 * @param {{ commandPaletteOpen?: boolean }} [options]
 * @returns {boolean}
 */
export function handleModEnterBlurCommit(event, options = {}) {
  const { commandPaletteOpen = false } = options;
  if (commandPaletteOpen) return false;
  if (event.defaultPrevented) return false;
  if (event.repeat) return false;
  if (event.key !== "Enter" || !(event.metaKey || event.ctrlKey)) return false;

  const target = findBlurShortcutTarget(event);
  if (!target) return false;
  if (hasTextShortcutOptOut(target)) return false;
  if (!target.closest(`[${MOD_ENTER_COMMIT_ATTR}="blur"]`)) return false;

  event.preventDefault();
  target.blur();
  return true;
}

/**
 * Escape on a control tagged with `data-anx-escape-dismiss="blur"` blurs it.
 *
 * @param {KeyboardEvent} event
 * @param {{ commandPaletteOpen?: boolean }} [options]
 * @returns {boolean}
 */
export function handleEscapeTextBlurCommit(event, options = {}) {
  const { commandPaletteOpen = false } = options;
  if (commandPaletteOpen) return false;
  if (event.defaultPrevented) return false;
  if (event.repeat) return false;
  if (event.key !== "Escape") return false;

  const target = findBlurShortcutTarget(event);
  if (!target) return false;
  if (hasTextShortcutOptOut(target)) return false;
  if (!target.closest(`[${ESCAPE_DISMISS_ATTR}="blur"]`)) return false;

  event.preventDefault();
  target.blur();
  return true;
}

/**
 * @param {Element} el
 * @returns {boolean}
 */
function hasTextShortcutOptOut(el) {
  if (!(el instanceof Element)) return false;
  return Boolean(
    el.closest(`[${TEXT_SHORTCUT_OPT_OUT}]`) ||
      el.closest(`[${FORM_SUBMIT_SHORTCUT_OPT_OUT}]`),
  );
}

/**
 * @param {KeyboardEvent} event
 * @returns {HTMLElement | null}
 */
function findBlurShortcutTarget(event) {
  const fromEvent = event.target instanceof Element ? event.target : null;
  if (fromEvent && isBlurCommitTextControl(fromEvent)) {
    return /** @type {HTMLElement} */ (fromEvent);
  }
  const active = document.activeElement;
  if (active instanceof HTMLElement && isBlurCommitTextControl(active)) {
    return active;
  }
  return null;
}

/**
 * @param {KeyboardEvent} event
 * @returns {HTMLElement | null}
 */
function findFormSubmitTextTarget(event) {
  const fromEvent = event.target instanceof Element ? event.target : null;
  if (fromEvent && isTextLikeFormControl(fromEvent)) {
    return /** @type {HTMLElement} */ (fromEvent);
  }
  const active = document.activeElement;
  if (active instanceof HTMLElement && isTextLikeFormControl(active)) {
    return active;
  }
  return null;
}

/**
 * Text-ish controls for blur-commit shortcuts: `input` (text-like), `textarea`, `contenteditable`.
 * Excludes `select` (native Escape is platform-owned).
 * @param {Element} el
 * @returns {boolean}
 */
function isBlurCommitTextControl(el) {
  if (!(el instanceof HTMLElement)) return false;
  if (!el.isConnected) return false;
  if (el.tagName === "SELECT") return false;

  if (el.isContentEditable) {
    return el.getAttribute("contenteditable") !== "false";
  }
  if (el instanceof HTMLTextAreaElement) {
    return !el.disabled && !el.readOnly;
  }
  if (el instanceof HTMLInputElement) {
    if (el.disabled || el.readOnly) return false;
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
  return false;
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
