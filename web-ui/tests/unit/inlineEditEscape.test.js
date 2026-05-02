// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { inlineEditEscape } from "../../src/lib/actions/inlineEditEscape.js";

describe("inlineEditEscape", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("on Escape calls onRevert then onAfter in order", () => {
    const order = [];
    const el = document.createElement("textarea");
    const api = inlineEditEscape(el, {
      onRevert: () => {
        order.push("revert");
      },
      onAfter: () => {
        order.push("after");
      },
    });

    el.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Escape",
        bubbles: true,
        cancelable: true,
      }),
    );

    expect(order).toEqual(["revert", "after"]);
    api.destroy();
  });

  it("calls preventDefault and stopPropagation on Escape", () => {
    const el = document.createElement("textarea");
    const api = inlineEditEscape(el, { onRevert: () => {} });

    const ev = new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true,
      cancelable: true,
    });
    const pd = vi.spyOn(ev, "preventDefault");
    const sp = vi.spyOn(ev, "stopPropagation");
    el.dispatchEvent(ev);
    expect(pd).toHaveBeenCalled();
    expect(sp).toHaveBeenCalled();
    api.destroy();
  });

  it("ignores non-Escape and repeat", () => {
    const revert = vi.fn();
    const el = document.createElement("input");
    inlineEditEscape(el, { onRevert: revert });

    el.dispatchEvent(new KeyboardEvent("keydown", { key: "a", bubbles: true }));
    expect(revert).not.toHaveBeenCalled();

    el.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Escape",
        repeat: true,
        bubbles: true,
      }),
    );
    expect(revert).not.toHaveBeenCalled();
  });

  it("update swaps callbacks; destroy removes handler", () => {
    const a = vi.fn();
    const b = vi.fn();
    const el = document.createElement("textarea");
    const api = inlineEditEscape(el, { onRevert: a });

    el.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    expect(a).toHaveBeenCalledTimes(1);

    api.update({ onRevert: b });
    el.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);

    api.destroy();
    el.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    expect(b).toHaveBeenCalledTimes(1);
  });

  it("disabled skips handling", () => {
    const revert = vi.fn();
    const el = document.createElement("textarea");
    const api = inlineEditEscape(el, { onRevert: revert, disabled: true });

    el.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    expect(revert).not.toHaveBeenCalled();

    api.destroy();
  });
});
