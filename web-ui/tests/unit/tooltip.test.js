// @vitest-environment jsdom
import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  TOOLTIP_DELAY_MS,
  TOOLTIP_HOLD_MS,
  activeTooltip,
  hideTooltip,
  tooltip,
  tooltipPosition,
} from "../../src/lib/actions/tooltip.js";

/**
 * The reason this module exists: the browser's own `title` waits about a
 * second and paints a question-mark cursor meanwhile, and neither is
 * configurable. These assert the two properties that made it worth replacing —
 * it opens fast, and the text is readable without hovering.
 */

function node(
  rect = { top: 100, bottom: 118, left: 200, width: 24, height: 18 },
) {
  const element = document.createElement("span");
  element.getBoundingClientRect = () => ({
    ...rect,
    right: rect.left + rect.width,
  });
  document.body.append(element);
  return element;
}

beforeEach(() => {
  vi.useFakeTimers();
  hideTooltip();
});

afterEach(() => {
  hideTooltip();
  vi.useRealTimers();
  document.body.replaceChildren();
});

describe("tooltip action", () => {
  it("opens well inside the 150ms the brief allows", () => {
    expect(TOOLTIP_DELAY_MS).toBeLessThanOrEqual(150);
    const element = node();
    tooltip(element, "Moved Oct 5, 2026, 11:12 (8h)");
    element.dispatchEvent(new Event("pointerenter"));
    expect(get(activeTooltip)).toBeNull();
    vi.advanceTimersByTime(TOOLTIP_DELAY_MS);
    expect(get(activeTooltip)?.text).toBe("Moved Oct 5, 2026, 11:12 (8h)");
  });

  it("opens with no delay at all on keyboard focus", () => {
    const element = node();
    tooltip(element, "Blocked — a step on the critical path is blocked.");
    element.dispatchEvent(new Event("focusin"));
    expect(get(activeTooltip)?.text).toContain("Blocked");
  });

  it("publishes the text as an attribute, so there is something to read", () => {
    const element = node();
    const action = tooltip(element, "Updated 3d ago");
    expect(element.getAttribute("data-tooltip")).toBe("Updated 3d ago");
    action.update("Updated 4d ago");
    expect(element.getAttribute("data-tooltip")).toBe("Updated 4d ago");
    action.update("");
    expect(element.hasAttribute("data-tooltip")).toBe(false);
  });

  it("closes on leave, on blur and on Escape", () => {
    const element = node();
    tooltip(element, "Last signal");
    for (const event of ["pointerleave", "focusout"]) {
      element.dispatchEvent(new Event("focusin"));
      expect(get(activeTooltip)).not.toBeNull();
      element.dispatchEvent(new Event(event));
      expect(get(activeTooltip)).toBeNull();
    }
    element.dispatchEvent(new Event("focusin"));
    element.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(get(activeTooltip)).toBeNull();
  });

  it("says nothing when there is nothing to say", () => {
    const element = node();
    tooltip(element, "   ");
    element.dispatchEvent(new Event("focusin"));
    expect(get(activeTooltip)).toBeNull();
  });

  it("does not open for a node the pointer has already left", () => {
    const element = node();
    tooltip(element, "Moved");
    element.dispatchEvent(new Event("pointerenter"));
    element.dispatchEvent(new Event("pointerleave"));
    vi.advanceTimersByTime(TOOLTIP_DELAY_MS * 4);
    expect(get(activeTooltip)).toBeNull();
  });

  it("pins the sentence on a touch tap and on a long-press", () => {
    const element = node();
    tooltip(element, "Oct 5, 2026, 1:56 PM GMT+7");
    const touch = (type) =>
      element.dispatchEvent(new PointerEvent(type, { pointerType: "touch" }));
    touch("pointerenter");
    touch("pointerdown");
    touch("pointerup");
    expect(get(activeTooltip)?.text).toBe("Oct 5, 2026, 1:56 PM GMT+7");
    element.dispatchEvent(new PointerEvent("pointerleave"));
    expect(get(activeTooltip)?.text).toBe("Oct 5, 2026, 1:56 PM GMT+7");

    // The browser sends pointerenter again before the dismissing tap.
    touch("pointerenter");
    touch("pointerdown");
    expect(get(activeTooltip)).toBeNull();

    touch("pointerenter");
    touch("pointerdown");
    vi.advanceTimersByTime(TOOLTIP_HOLD_MS);
    expect(get(activeTooltip)?.text).toContain("Oct 5");
  });

  it("cancels the touch that reveals the tip so a parent link is not activated", () => {
    const parent = document.createElement("a");
    const element = node();
    parent.append(element);
    document.body.append(parent);
    let parentDown = 0;
    parent.addEventListener("pointerdown", () => {
      parentDown += 1;
    });
    tooltip(element, "Oct 5, 2026, 1:56 PM GMT+7");
    const down = new PointerEvent("pointerdown", {
      pointerType: "touch",
      bubbles: true,
      cancelable: true,
    });
    element.dispatchEvent(down);
    expect(down.defaultPrevented).toBe(true);
    expect(parentDown).toBe(0);

    const link = document.createElement("a");
    document.body.append(link);
    tooltip(link, "Answer in Inbox");
    const linkDown = new PointerEvent("pointerdown", {
      pointerType: "touch",
      bubbles: true,
      cancelable: true,
    });
    link.dispatchEvent(linkDown);
    expect(linkDown.defaultPrevented).toBe(false);
  });

  it("still dismisses a mouse press immediately", () => {
    const element = node();
    tooltip(element, "Oct 5, 2026, 1:56 PM GMT+7");
    element.dispatchEvent(new Event("focusin"));
    expect(get(activeTooltip)).not.toBeNull();
    element.dispatchEvent(
      new PointerEvent("pointerdown", { pointerType: "mouse" }),
    );
    expect(get(activeTooltip)).toBeNull();
  });

  it("stops reporting once destroyed", () => {
    const element = node();
    const action = tooltip(element, "Moved");
    action.destroy();
    element.dispatchEvent(new Event("focusin"));
    expect(get(activeTooltip)).toBeNull();
  });
});

describe("tooltipPosition", () => {
  const viewport = { width: 800, height: 600 };
  const size = { width: 120, height: 24 };

  it("sits above the trigger and centred on it", () => {
    const place = tooltipPosition(
      { top: 200, bottom: 218, left: 300, width: 40, height: 18 },
      size,
      viewport,
    );
    expect(place.placement).toBe("top");
    expect(place.top).toBe(200 - 24 - 6);
    expect(place.left).toBe(300 + 20 - 60);
  });

  it("flips below when there is no room above", () => {
    const place = tooltipPosition(
      { top: 4, bottom: 22, left: 300, width: 40, height: 18 },
      size,
      viewport,
    );
    expect(place.placement).toBe("bottom");
    expect(place.top).toBe(28);
  });

  it("never hangs off the side of the window", () => {
    const left = tooltipPosition(
      { top: 200, bottom: 218, left: 0, width: 16, height: 18 },
      size,
      viewport,
    );
    expect(left.left).toBe(8);
    const right = tooltipPosition(
      { top: 200, bottom: 218, left: 790, width: 16, height: 18 },
      size,
      viewport,
    );
    expect(right.left).toBe(viewport.width - size.width - 8);
  });
});
