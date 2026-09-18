// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";

import { focusTrap } from "../../src/lib/actions/focusTrap.js";

/** @type {Array<{ destroy: () => void }>} */
let traps = [];

function mountDialog(inner) {
  const outside = document.createElement("button");
  outside.textContent = "outside";
  document.body.append(outside);

  const dialog = document.createElement("div");
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  dialog.innerHTML = inner;
  document.body.append(dialog);
  return { dialog, outside };
}

function tab({ shiftKey = false } = {}) {
  const target = document.activeElement ?? document.body;
  const event = new KeyboardEvent("keydown", {
    key: "Tab",
    shiftKey,
    bubbles: true,
    cancelable: true,
  });
  target.dispatchEvent(event);
  return event;
}

afterEach(() => {
  for (const trap of traps) trap.destroy();
  traps = [];
  document.body.innerHTML = "";
});

/** @param {HTMLElement} node */
function trapOn(node, options) {
  const trap = focusTrap(node, options);
  traps.push(trap);
  return trap;
}

describe("focusTrap", () => {
  it("moves focus to the first tabbable control on open", () => {
    const { dialog } = mountDialog(
      '<input id="search" /><button id="first">one</button>',
    );
    trapOn(dialog);
    expect(document.activeElement?.id).toBe("search");
  });

  it("honours initialFocus over document order", () => {
    const { dialog } = mountDialog(
      '<button id="first">one</button><input id="search" />',
    );
    trapOn(dialog, {
      initialFocus: () => dialog.querySelector("#search"),
    });
    expect(document.activeElement?.id).toBe("search");
  });

  it("cycles Tab and Shift+Tab inside the dialog", () => {
    const { dialog } = mountDialog(
      '<input id="search" /><button id="one">one</button><button id="two">two</button>',
    );
    trapOn(dialog);
    expect(document.activeElement?.id).toBe("search");

    // Forward from the last control wraps to the first.
    dialog.querySelector("#two").focus();
    const forward = tab();
    expect(forward.defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe("search");

    // Backward from the first control wraps to the last.
    const back = tab({ shiftKey: true });
    expect(back.defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe("two");
  });

  it("leaves Tab alone between controls in the middle of the dialog", () => {
    const { dialog } = mountDialog(
      '<input id="search" /><button id="one">one</button><button id="two">two</button>',
    );
    trapOn(dialog);
    const event = tab();
    expect(event.defaultPrevented).toBe(false);
  });

  it("pulls focus back when it has escaped the dialog", () => {
    const { dialog, outside } = mountDialog(
      '<input id="search" /><button id="one">one</button>',
    );
    trapOn(dialog);
    outside.focus();
    const event = tab();
    expect(event.defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe("search");
  });

  it("moves Tab from the dialog node into the first tabbable control", () => {
    const { dialog } = mountDialog(
      '<input id="search" /><button id="one">one</button>',
    );
    dialog.tabIndex = -1;
    trapOn(dialog);
    dialog.focus();
    expect(document.activeElement).toBe(dialog);

    const forward = tab();
    expect(forward.defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe("search");

    dialog.focus();
    const back = tab({ shiftKey: true });
    expect(back.defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe("one");
  });

  it("skips disabled controls when cycling", () => {
    const { dialog } = mountDialog(
      '<input id="search" /><button id="one">one</button><button id="hint" disabled>hint</button>',
    );
    trapOn(dialog);
    dialog.querySelector("#one").focus();
    tab();
    expect(document.activeElement?.id).toBe("search");
  });

  it("holds focus on the dialog when it has no tabbable control", () => {
    const { dialog } = mountDialog("<p>nothing to focus</p>");
    trapOn(dialog);
    expect(document.activeElement).toBe(dialog);
    expect(dialog.getAttribute("tabindex")).toBe("-1");
    const event = tab();
    expect(event.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(dialog);
  });

  it("restores focus to the opener on destroy", () => {
    const { dialog, outside } = mountDialog('<input id="search" />');
    outside.focus();
    const trap = focusTrap(dialog, {});
    expect(document.activeElement?.id).toBe("search");
    trap.destroy();
    expect(document.activeElement).toBe(outside);
  });

  it("falls back when the palette was opened with nothing focused", () => {
    const { dialog } = mountDialog('<input id="search" />');
    const main = document.createElement("main");
    document.body.append(main);
    document.body.focus();

    const trap = focusTrap(dialog, { fallbackFocus: () => main });
    trap.destroy();
    expect(document.activeElement).toBe(main);
    // A landmark has to be made programmatically focusable to receive focus.
    expect(main.getAttribute("tabindex")).toBe("-1");
  });

  it("leaves a natively focusable fallback out of tabindex rewriting", () => {
    const { dialog, outside } = mountDialog('<input id="search" />');
    outside.remove();
    document.body.focus();
    const trigger = document.createElement("button");
    document.body.append(trigger);

    const trap = focusTrap(dialog, { fallbackFocus: () => trigger });
    trap.destroy();
    expect(document.activeElement).toBe(trigger);
    expect(trigger.hasAttribute("tabindex")).toBe(false);
  });

  it("stops trapping once disabled and releases the keyboard", () => {
    const { dialog, outside } = mountDialog('<input id="search" />');
    outside.focus();
    const trap = trapOn(dialog);
    trap.update({ enabled: false });
    expect(document.activeElement).toBe(outside);
    expect(tab().defaultPrevented).toBe(false);
  });
});
