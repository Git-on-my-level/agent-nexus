// @vitest-environment jsdom
import {
  cleanup,
  createEvent,
  fireEvent,
  render,
  screen,
} from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import { tick } from "svelte";

import DashboardPicker from "../../src/lib/components/overview/DashboardPicker.svelte";
import InfoTip from "../../src/lib/components/InfoTip.svelte";

afterEach(cleanup);

const report = (id, title) => ({
  id,
  title,
  ref: `document:${id}`,
  segment: id,
});
const REPORTS = [report("today", "Today"), report("older", "Earlier")];

/**
 * `rerender` replaces the whole prop set, so the base travels with it: a
 * partial rerender would unmount the menu and the test would pass for the
 * wrong reason.
 */
const open = async (props = {}) => {
  const base = {
    reports: REPORTS,
    selected: REPORTS[0],
    pinnedRef: "",
    hasMore: false,
    loading: false,
    ...props,
  };
  const result = render(DashboardPicker, base);
  await fireEvent.click(screen.getByRole("button", { name: /Today/ }));
  return { ...result, update: (next) => result.rerender({ ...base, ...next }) };
};

describe("the dashboard picker", () => {
  it("lists every loaded report and names the current one", async () => {
    await open();
    expect(
      screen.getAllByRole("menuitemradio").map((node) => node.textContent),
    ).toEqual([
      expect.stringContaining("Today"),
      expect.stringContaining("Earlier"),
    ]);
    expect(screen.getByRole("menuitemradio", { checked: true })).toHaveProperty(
      "dataset.overviewReportChoice",
      "today",
    );
  });

  it("asks for more choices once per open, not once per render", async () => {
    const onload = vi.fn();
    const { update } = await open({ hasMore: true, onload });
    expect(onload).toHaveBeenCalledTimes(1);
    // A prop change while the menu is open must not re-ask.
    await update({ reports: [...REPORTS, report("third", "Third")] });
    expect(onload).toHaveBeenCalledTimes(1);
    // The in-menu item is the way to ask for the next page.
    await fireEvent.click(
      screen.getByRole("menuitem", { name: "Load more reports" }),
    );
    expect(onload).toHaveBeenCalledTimes(2);
  });

  it("selects a report and gives the trigger its focus back", async () => {
    const onselect = vi.fn();
    await open({ onselect });
    await fireEvent.click(
      screen.getByRole("menuitemradio", { name: /Earlier/ }),
    );
    expect(onselect).toHaveBeenCalledWith("older");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: /Today/ }),
    );
  });

  it("does not re-select the report already showing", async () => {
    const onselect = vi.fn();
    await open({ onselect });
    await fireEvent.click(screen.getByRole("menuitemradio", { name: /Today/ }));
    expect(onselect).not.toHaveBeenCalled();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("closes on an outside pointer, and not on one inside", async () => {
    await open();
    await fireEvent.pointerDown(screen.getByRole("menu"));
    await tick();
    expect(screen.queryByRole("menu")).not.toBeNull();
    await fireEvent.pointerDown(document.body);
    await tick();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("closes when focus leaves, so it cannot sit open eating Escape", async () => {
    const outside = document.createElement("button");
    document.body.append(outside);
    await open();
    await fireEvent.focusOut(screen.getByRole("menu"), {
      relatedTarget: outside,
    });
    await tick();
    expect(screen.queryByRole("menu")).toBeNull();
    outside.remove();
  });

  it("pins from the menu and closes on the click", async () => {
    const onpin = vi.fn();
    await open({ onpin });
    await fireEvent.click(
      screen.getByRole("menuitem", { name: "Pin as dashboard" }),
    );
    expect(onpin).toHaveBeenCalledWith("document:today");
    // The write reports itself in the header beside this control, not in a
    // popover the click has already closed.
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: /Today/ }),
    );
  });

  it("offers the way back once a report is pinned", async () => {
    await open({ pinnedRef: "document:today" });
    expect(
      screen.getByRole("menuitem", { name: "Use newest report" }),
    ).toBeTruthy();
    expect(screen.getAllByText("Pinned").length).toBeGreaterThan(0);
  });

  it("owns only menu roles, so a menu never claims a list as its child", async () => {
    await open();
    const menu = screen.getByRole("menu");
    for (const node of menu.querySelectorAll("ul, li, div"))
      expect(node.getAttribute("role")).toBe("none");
  });
});

describe("the dashboard picker's keyboard", () => {
  /*
   * `role="menu"` promises the menu-button pattern, and replacing a native
   * `<select>` means inheriting what a select already did. Tab alone was a
   * workaround, not the contract.
   */
  const render1 = (props = {}) =>
    render(DashboardPicker, {
      reports: REPORTS,
      selected: REPORTS[0],
      pinnedRef: "",
      hasMore: false,
      loading: false,
      ...props,
    });
  const triggerOf = () => screen.getByRole("button", { name: /Today/ });
  const itemsOf = () =>
    [...document.querySelectorAll("[data-picker-item]")].filter(Boolean);

  for (const key of ["ArrowDown", "Enter", " "]) {
    it(`opens on ${key === " " ? "Space" : key} and focuses the first item`, async () => {
      render1();
      await fireEvent.keyDown(triggerOf(), { key });
      await tick();
      expect(screen.getByRole("menu")).toBeTruthy();
      expect(document.activeElement).toBe(itemsOf()[0]);
    });
  }

  it("opens on ArrowUp and focuses the last item", async () => {
    render1();
    await fireEvent.keyDown(triggerOf(), { key: "ArrowUp" });
    await tick();
    const list = itemsOf();
    expect(document.activeElement).toBe(list[list.length - 1]);
  });

  it("walks the items with the arrows, and wraps", async () => {
    render1();
    await fireEvent.keyDown(triggerOf(), { key: "ArrowDown" });
    await tick();
    const list = itemsOf();
    const menu = screen.getByRole("menu");
    await fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(list[1]);
    await fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(document.activeElement).toBe(list[0]);
    // Past the top is the bottom, as a menu wraps.
    await fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(document.activeElement).toBe(list[list.length - 1]);
  });

  it("jumps to the ends with Home and End", async () => {
    render1();
    await fireEvent.keyDown(triggerOf(), { key: "ArrowDown" });
    await tick();
    const list = itemsOf();
    const menu = screen.getByRole("menu");
    await fireEvent.keyDown(menu, { key: "End" });
    expect(document.activeElement).toBe(list[list.length - 1]);
    await fireEvent.keyDown(menu, { key: "Home" });
    expect(document.activeElement).toBe(list[0]);
  });

  it("chooses the focused report, and the menu does not swallow the key", async () => {
    const onselect = vi.fn();
    render1({ onselect });
    await fireEvent.keyDown(triggerOf(), { key: "ArrowDown" });
    await tick();
    await fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });
    const item = document.activeElement;
    /*
     * The menu's own handler must leave Enter alone: the items are real
     * buttons, so the browser turns Enter into their click. jsdom does not,
     * which is what the e2e keyboard walk is for — here we check only that
     * nothing cancelled it on the way through.
     */
    const enter = createEvent.keyDown(item, {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    });
    await fireEvent(item, enter);
    expect(enter.defaultPrevented).toBe(false);
    await fireEvent.click(item);
    expect(onselect).toHaveBeenCalledWith("older");
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("closes on Tab so the reader leaves rather than cycling the menu", async () => {
    render1();
    await fireEvent.keyDown(triggerOf(), { key: "ArrowDown" });
    await tick();
    await fireEvent.keyDown(screen.getByRole("menu"), { key: "Tab" });
    await tick();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("closes on Escape and gives the trigger its focus back", async () => {
    render1();
    await fireEvent.keyDown(triggerOf(), { key: "ArrowDown" });
    await tick();
    await fireEvent.keyDown(document, { key: "Escape" });
    await tick();
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(triggerOf());
  });

  it("every item is reachable only through the menu, never by Tab", async () => {
    render1();
    await fireEvent.keyDown(triggerOf(), { key: "ArrowDown" });
    await tick();
    for (const item of itemsOf())
      expect(item.getAttribute("tabindex")).toBe("-1");
  });

  it("keeps focus in the menu when asking for more choices", async () => {
    /*
     * "Load more reports" is replaced by the loading note the moment it is
     * clicked, so the element holding focus leaves the DOM. Without re-aiming
     * focus it falls to the body, outside the menu, where the arrow keys no
     * longer reach it.
     */
    const onload = vi.fn();
    const { rerender } = render1({ hasMore: true, onload });
    await fireEvent.keyDown(triggerOf(), { key: "ArrowDown" });
    await tick();
    const more = screen.getByRole("menuitem", { name: "Load more reports" });
    // Focus it the way the keyboard would: a dispatched click does not move
    // focus in jsdom, so without this the test passes however the component
    // behaves — the first report would still be holding focus at assert time.
    more.focus();
    expect(document.activeElement).toBe(more);
    await fireEvent.click(more);
    // The parent flips `loading` synchronously, unmounting the focused item.
    await rerender({
      reports: REPORTS,
      selected: REPORTS[0],
      pinnedRef: "",
      hasMore: true,
      loading: true,
      onload,
    });
    await tick();
    expect(document.body.contains(more)).toBe(false);
    expect(screen.getByRole("menu").contains(document.activeElement)).toBe(
      true,
    );
  });

  it("asks for the choices once when a key opens it", async () => {
    const onload = vi.fn();
    render1({ hasMore: true, onload });
    await fireEvent.keyDown(triggerOf(), { key: "ArrowDown" });
    await tick();
    expect(onload).toHaveBeenCalledTimes(1);
  });
});

describe("an info tip", () => {
  it("carries its sentence as the tooltip and the accessible name", () => {
    render(InfoTip, { label: "What this means", text: "A short answer." });
    const tip = screen.getByRole("button", { name: /What this means/ });
    expect(tip.getAttribute("data-tooltip")).toBe("A short answer.");
    expect(tip.getAttribute("aria-label")).toBe(
      "What this means: A short answer.",
    );
  });

  it("renders nothing without a sentence", () => {
    render(InfoTip, { label: "What this means", text: "   " });
    expect(screen.queryByRole("button")).toBeNull();
  });
});
