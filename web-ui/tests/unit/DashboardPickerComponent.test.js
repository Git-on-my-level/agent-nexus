// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
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
