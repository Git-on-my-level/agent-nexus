// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ESCAPE_DISMISS_ATTR,
  SAVE_SHORTCUT_ATTR,
  TEXT_SHORTCUT_OPT_OUT,
  handleEscapeTextBlurCommit,
  handleModEnterBlurCommit,
  handleModEnterFormSubmit,
  handleModSave,
} from "../../src/lib/formSubmitShortcut.js";

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

function modEnterEvent(target, extra = {}) {
  return new KeyboardEvent("keydown", {
    key: "Enter",
    metaKey: true,
    bubbles: true,
    cancelable: true,
    ...extra,
  });
}

function escapeEvent(target, extra = {}) {
  return new KeyboardEvent("keydown", {
    key: "Escape",
    bubbles: true,
    cancelable: true,
    ...extra,
  });
}

function modSaveEvent(extra = {}) {
  return new KeyboardEvent("keydown", {
    key: "s",
    metaKey: true,
    bubbles: true,
    cancelable: true,
    ...extra,
  });
}

describe("handleModSave", () => {
  it("clicks the save button scoped to focused control", () => {
    document.body.innerHTML = `
      <div data-anx-save-scope>
        <textarea id="editor"></textarea>
        <button id="save" ${SAVE_SHORTCUT_ATTR} type="button">Save</button>
      </div>
    `;
    const ta = /** @type {HTMLTextAreaElement} */ (
      document.getElementById("editor")
    );
    const save = /** @type {HTMLButtonElement} */ (
      document.getElementById("save")
    );
    const clickSpy = vi.spyOn(save, "click");
    ta.focus();

    const ev = modSaveEvent();
    expect(handleModSave(ev, {})).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
    expect(clickSpy).toHaveBeenCalled();
  });

  it("returns false when save button is disabled", () => {
    document.body.innerHTML = `
      <div data-anx-save-scope>
        <textarea id="editor"></textarea>
        <button id="save" ${SAVE_SHORTCUT_ATTR} type="button" disabled>Save</button>
      </div>
    `;
    const ta = document.getElementById("editor");
    ta.focus();
    const ev = modSaveEvent();
    expect(handleModSave(ev, {})).toBe(false);
    expect(ev.defaultPrevented).toBe(false);
  });

  it("returns false when commandPaletteOpen", () => {
    document.body.innerHTML = `
      <button id="save" ${SAVE_SHORTCUT_ATTR} type="button">Save</button>
    `;
    const ev = modSaveEvent();
    expect(handleModSave(ev, { commandPaletteOpen: true })).toBe(false);
  });

  it("prefers save button inside an open dialog", () => {
    document.body.innerHTML = `
      <button id="page-save" ${SAVE_SHORTCUT_ATTR} type="button">Page</button>
      <div role="dialog">
        <button id="dialog-save" ${SAVE_SHORTCUT_ATTR} type="button">Dialog</button>
      </div>
    `;
    const dialogSave = /** @type {HTMLButtonElement} */ (
      document.getElementById("dialog-save")
    );
    const clickSpy = vi.spyOn(dialogSave, "click");

    const ev = modSaveEvent();
    expect(handleModSave(ev, {})).toBe(true);
    expect(clickSpy).toHaveBeenCalled();
  });

  it("clicks the only enabled save button on the page", () => {
    document.body.innerHTML = `
      <button id="save" ${SAVE_SHORTCUT_ATTR} type="button">Save</button>
    `;
    const save = /** @type {HTMLButtonElement} */ (
      document.getElementById("save")
    );
    const clickSpy = vi.spyOn(save, "click");
    const ev = modSaveEvent();
    expect(handleModSave(ev, {})).toBe(true);
    expect(clickSpy).toHaveBeenCalled();
  });

  it("does not use singleton fallback when focus is in a modal without save", () => {
    document.body.innerHTML = `
      <button id="page-save" ${SAVE_SHORTCUT_ATTR} type="button">Save</button>
      <div role="dialog" aria-modal="true">
        <button id="cancel" type="button">Cancel</button>
      </div>
    `;
    const pageSave = /** @type {HTMLButtonElement} */ (
      document.getElementById("page-save")
    );
    const cancel = document.getElementById("cancel");
    const clickSpy = vi.spyOn(pageSave, "click");
    cancel.focus();

    const ev = modSaveEvent();
    expect(handleModSave(ev, {})).toBe(false);
    expect(clickSpy).not.toHaveBeenCalled();
  });
});

describe("handleModEnterFormSubmit", () => {
  it("calls requestSubmit and prevents default when input is in form", () => {
    document.body.innerHTML = `
      <form id="f"><input type="text" id="t" /></form>
    `;
    const form = /** @type {HTMLFormElement} */ (document.getElementById("f"));
    const input = /** @type {HTMLInputElement} */ (
      document.getElementById("t")
    );
    const submit = vi.fn((e) => e.preventDefault());
    form.addEventListener("submit", submit);
    const spy = vi.spyOn(form, "requestSubmit").mockImplementation(() => {});
    input.focus();

    const ev = new KeyboardEvent("keydown", {
      key: "Enter",
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    });
    const handled = handleModEnterFormSubmit(ev, { commandPaletteOpen: false });

    expect(handled).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
    expect(spy).toHaveBeenCalled();
    expect(submit).not.toHaveBeenCalled();
  });

  it("returns false when commandPaletteOpen", () => {
    document.body.innerHTML = `<form><input type="text" id="t" /></form>`;
    const input = document.getElementById("t");
    input.focus();
    const ev = modEnterEvent(input);
    expect(handleModEnterFormSubmit(ev, { commandPaletteOpen: true })).toBe(
      false,
    );
    expect(ev.defaultPrevented).toBe(false);
  });

  it("returns false when defaultPrevented", () => {
    document.body.innerHTML = `<form><input type="text" id="t" /></form>`;
    const input = document.getElementById("t");
    input.focus();
    const ev = modEnterEvent(input);
    ev.preventDefault();
    expect(handleModEnterFormSubmit(ev, { commandPaletteOpen: false })).toBe(
      false,
    );
  });

  it("ignores form under no-submit-shortcut", () => {
    document.body.innerHTML = `
      <form id="f" data-anx-no-submit-shortcut><input type="text" id="t" /></form>
    `;
    const form = /** @type {HTMLFormElement} */ (document.getElementById("f"));
    vi.spyOn(form, "requestSubmit").mockImplementation(() => {});
    const input = document.getElementById("t");
    input.focus();
    const ev = modEnterEvent(input);
    expect(handleModEnterFormSubmit(ev, {})).toBe(false);
    expect(ev.defaultPrevented).toBe(false);
  });

  it("ignores when focused control has no-submit-shortcut", () => {
    document.body.innerHTML = `
      <form id="f"><input type="text" id="t" data-anx-no-submit-shortcut /></form>
    `;
    const form = /** @type {HTMLFormElement} */ (document.getElementById("f"));
    vi.spyOn(form, "requestSubmit").mockImplementation(() => {});
    const input = document.getElementById("t");
    input.focus();
    const ev = modEnterEvent(input);
    expect(handleModEnterFormSubmit(ev, {})).toBe(false);
  });
});

describe("handleModEnterBlurCommit", () => {
  it("blurs control when ancestor has mod-enter-commit blur", () => {
    document.body.innerHTML = `
      <div data-anx-mod-enter-commit="blur">
        <textarea id="a"></textarea>
      </div>
    `;
    const ta = /** @type {HTMLTextAreaElement} */ (
      document.getElementById("a")
    );
    const blurSpy = vi.spyOn(ta, "blur");
    ta.focus();
    expect(document.activeElement).toBe(ta);

    const ev = modEnterEvent(ta);
    expect(handleModEnterBlurCommit(ev, {})).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
    expect(blurSpy).toHaveBeenCalled();
  });

  it("ignores untagged control", () => {
    document.body.innerHTML = `<textarea id="a"></textarea>`;
    const ta = document.getElementById("a");
    ta.focus();
    const ev = modEnterEvent(ta);
    expect(handleModEnterBlurCommit(ev, {})).toBe(false);
  });

  it("honors text shortcut opt-out", () => {
    document.body.innerHTML = `
      <div data-anx-mod-enter-commit="blur" ${TEXT_SHORTCUT_OPT_OUT}>
        <textarea id="a"></textarea>
      </div>
    `;
    const ta = document.getElementById("a");
    ta.focus();
    const ev = modEnterEvent(ta);
    expect(handleModEnterBlurCommit(ev, {})).toBe(false);
  });

  it("honors legacy no-submit-shortcut on ancestor for blur commit", () => {
    document.body.innerHTML = `
      <div data-anx-mod-enter-commit="blur" data-anx-no-submit-shortcut>
        <textarea id="a"></textarea>
      </div>
    `;
    const ta = document.getElementById("a");
    ta.focus();
    const ev = modEnterEvent(ta);
    expect(handleModEnterBlurCommit(ev, {})).toBe(false);
  });

  it("returns false when commandPaletteOpen", () => {
    document.body.innerHTML = `
      <div data-anx-mod-enter-commit="blur"><textarea id="a"></textarea></div>
    `;
    const ta = document.getElementById("a");
    ta.focus();
    const ev = modEnterEvent(ta);
    expect(handleModEnterBlurCommit(ev, { commandPaletteOpen: true })).toBe(
      false,
    );
  });

  it("handles Ctrl+Enter same as Meta+Enter", () => {
    document.body.innerHTML = `
      <div data-anx-mod-enter-commit="blur"><textarea id="a"></textarea></div>
    `;
    const ta = /** @type {HTMLTextAreaElement} */ (
      document.getElementById("a")
    );
    const blurSpy = vi.spyOn(ta, "blur");
    ta.focus();
    const ev = new KeyboardEvent("keydown", {
      key: "Enter",
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    });
    expect(handleModEnterBlurCommit(ev, {})).toBe(true);
    expect(blurSpy).toHaveBeenCalled();
  });
});

describe("handleEscapeTextBlurCommit", () => {
  it("returns false when commandPaletteOpen", () => {
    document.body.innerHTML = `
      <div ${ESCAPE_DISMISS_ATTR}="blur"><input type="text" id="i" /></div>
    `;
    const inp = document.getElementById("i");
    inp.focus();
    const ev = escapeEvent(inp);
    expect(handleEscapeTextBlurCommit(ev, { commandPaletteOpen: true })).toBe(
      false,
    );
  });
  it("blurs control when ancestor has escape-dismiss blur", () => {
    document.body.innerHTML = `
      <div ${ESCAPE_DISMISS_ATTR}="blur">
        <input type="text" id="i" />
      </div>
    `;
    const inp = /** @type {HTMLInputElement} */ (document.getElementById("i"));
    const blurSpy = vi.spyOn(inp, "blur");
    inp.focus();
    const ev = escapeEvent(inp);
    expect(handleEscapeTextBlurCommit(ev, {})).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
    expect(blurSpy).toHaveBeenCalled();
  });

  it("works for contenteditable host with escape-dismiss", () => {
    document.body.innerHTML = `
      <div data-anx-escape-dismiss="blur" contenteditable="true" id="ed"></div>
    `;
    const ed = /** @type {HTMLElement} */ (document.getElementById("ed"));
    ed.focus();
    const ev = escapeEvent(ed);
    expect(handleEscapeTextBlurCommit(ev, {})).toBe(true);
    expect(ev.defaultPrevented).toBe(true);
  });

  it("ignores untagged control", () => {
    document.body.innerHTML = `<input type="text" id="i" />`;
    const inp = document.getElementById("i");
    inp.focus();
    const ev = escapeEvent(inp);
    expect(handleEscapeTextBlurCommit(ev, {})).toBe(false);
  });
});
