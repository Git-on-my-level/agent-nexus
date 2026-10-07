// @vitest-environment jsdom
import { cleanup, fireEvent, render, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import AnxRefPreview from "../../src/lib/components/AnxRefPreview.svelte";
import {
  refResolveExample,
  refResolveExampleRequest,
} from "../../src/lib/fixtures/refResolveExample.js";
import { indexResolvedRefs, refChipModel } from "../../src/lib/refResolve.js";

const resolved = indexResolvedRefs(refResolveExample, refResolveExampleRequest);
const context = { organizationSlug: "scaling", workspaceSlug: "anx" };
const model = (ref) => refChipModel(ref, resolved, context);

/** A stand-in chip for the preview to position against. */
function anchor() {
  const element = document.createElement("span");
  element.getBoundingClientRect = () => ({
    top: 100,
    bottom: 116,
    left: 40,
    right: 160,
    width: 120,
    height: 16,
  });
  document.body.append(element);
  return element;
}

beforeEach(() => {
  window.innerWidth = 1280;
  window.innerHeight = 900;
});

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

describe("AnxRefPreview", () => {
  it("renders nothing until a chip opens it", () => {
    const { container } = render(AnxRefPreview);
    expect(container.querySelector(".anx-ref-preview")).toBeNull();
  });

  it("shows what batch ref resolve returns: kind, status, progress and owner", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());

    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    const text = container.querySelector(".anx-ref-preview").textContent;
    expect(text).toContain("Initiative plans on cards");
    expect(text).toContain("Task");
    expect(text).toContain("in progress");
    expect(text).toContain("3/7");
    expect(text).toContain("Codex Sol");

    const progress = container.querySelector("progress");
    expect(progress.getAttribute("value")).toBe("3");
    expect(progress.getAttribute("max")).toBe("7");
  });

  it("shows the rows the contract now supplies", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    const text = container.querySelector(".anx-ref-preview").textContent;
    expect(text).toContain("Release B");
    expect(text).toContain("p1");
    expect(text).toContain("Next: Computed progress and health");
    // The age is a badge now: "21h", with "Moved <timestamp>" on hover. The
    // tooltip is ours rather than the browser's `title`, so it is readable
    // from `data-tooltip` and shows in 60ms instead of a second.
    const age = container.querySelector("time.age-badge");
    expect(age?.textContent?.trim()).toMatch(/^\d+[mhdwy]$|^now$/);
    expect(age?.getAttribute("title")).toBeNull();
    expect(age?.getAttribute("data-tooltip")).toMatch(/^Moved /);
  });

  it("omits a row the response did not carry", async () => {
    // Board metadata needs independent board visibility, so a readable ref can
    // arrive without one.
    const { container, component } = render(AnxRefPreview);
    component.open(model("doc:release-b-plan"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    expect(
      container.querySelector(".anx-ref-preview").textContent,
    ).not.toContain("Next:");
  });

  it("offers Open and Copy ref for a resolvable ref", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());

    await waitFor(() =>
      expect(
        container.querySelector(".anx-ref-preview__actions"),
      ).not.toBeNull(),
    );
    const open = container.querySelector(".anx-ref-preview__actions a");
    expect(open.textContent.trim()).toBe("Open");
    expect(open.getAttribute("href")).toBe(
      "/o/scaling/w/anx/tasks/initiative-plans",
    );
    expect(
      container.querySelector(".anx-ref-preview__actions button").textContent,
    ).toContain("Copy ref");
  });

  it("says a ref does not resolve, and offers no Open", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:deleted-thing"), anchor());

    await waitFor(() =>
      expect(
        container.querySelector(".anx-ref-preview__missing"),
      ).not.toBeNull(),
    );
    expect(
      container.querySelector(".anx-ref-preview__missing").textContent,
    ).toContain("does not resolve");
    expect(container.querySelector(".anx-ref-preview__actions a")).toBeNull();
    // Copy ref stays: the raw ref is the one useful thing about a dead ref.
    expect(
      container.querySelector(".anx-ref-preview__actions button"),
    ).not.toBeNull();
  });

  it("opens an external ref in a new tab", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(
      model("https://github.com/Git-on-my-level/agent-nexus/pull/246"),
      anchor(),
    );
    await waitFor(() =>
      expect(
        container.querySelector(".anx-ref-preview__actions a"),
      ).not.toBeNull(),
    );
    const open = container.querySelector(".anx-ref-preview__actions a");
    expect(open.getAttribute("target")).toBe("_blank");
    expect(open.getAttribute("rel")).toBe("noreferrer noopener");
  });

  it("omits the progress bar for a ref with no checklist", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(model("doc:release-b-plan"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    expect(container.querySelector("progress")).toBeNull();
  });

  it("closes when asked", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    component.close();
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).toBeNull(),
    );
  });

  it("copies the raw ref and confirms", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });

    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());
    await waitFor(() =>
      expect(
        container.querySelector(".anx-ref-preview__actions button"),
      ).not.toBeNull(),
    );
    container.querySelector(".anx-ref-preview__actions button").click();
    expect(writeText).toHaveBeenCalledWith("card:initiative-plans");
    await waitFor(() =>
      expect(
        container.querySelector(".anx-ref-preview__actions button").textContent,
      ).toContain("Copied"),
    );
  });

  /**
   * jsdom does no layout, so the card measures 0×0 and the placement branches
   * would never be reached. Give it a real box for the geometry tests.
   */
  function stubCardBox({ width = 352, height = 180 } = {}) {
    const original = Element.prototype.getBoundingClientRect;
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(
      function rect() {
        if (this.classList?.contains("anx-ref-preview")) {
          return {
            top: 0,
            bottom: height,
            left: 0,
            right: width,
            width,
            height,
          };
        }
        return original.call(this);
      },
    );
  }

  it("flips above the chip when there is no room below", async () => {
    window.innerHeight = 200;
    stubCardBox({ height: 80 });
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    const top = Number.parseFloat(
      container.querySelector(".anx-ref-preview").style.top,
    );
    // Below would start at 122 and an 80px card would run past a 200px
    // viewport, so it sits above the chip instead: 100 - 80 - 6.
    expect(top).toBe(14);
    vi.restoreAllMocks();
  });

  it("opens below the chip when there is room", async () => {
    window.innerHeight = 900;
    stubCardBox({ height: 180 });
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    expect(
      Number.parseFloat(container.querySelector(".anx-ref-preview").style.top),
    ).toBe(122);
    vi.restoreAllMocks();
  });

  it("pulls back inside the viewport for a chip near the right edge", async () => {
    window.innerWidth = 360;
    stubCardBox({ width: 344, height: 180 });
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    const left = Number.parseFloat(
      container.querySelector(".anx-ref-preview").style.left,
    );
    // Chip starts at 40; a 344px card on a 360px screen has to shift to 8.
    expect(left).toBe(8);
    vi.restoreAllMocks();
  });

  it("renders on an opaque surface, as the floating-layer guard requires", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    // The rule bans a translucent fill on a fixed layer; this uses --panel.
    const styles = container.querySelector(".anx-ref-preview").outerHTML;
    expect(styles).not.toContain("transparent");
  });
});

describe("AnxRefPreview stays reachable", () => {
  it("is a dialog, not a tooltip: it holds controls a reader moves into", async () => {
    const { container, component } = render(AnxRefPreview);
    component.open(model("card:initiative-plans"), anchor());
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );
    const card = container.querySelector(".anx-ref-preview");
    expect(card.getAttribute("role")).toBe("dialog");
    expect(card.getAttribute("aria-label")).toContain(
      "Initiative plans on cards",
    );
  });

  it("survives the gap between leaving the chip and reaching the card", async () => {
    vi.useFakeTimers();
    try {
      const { container, component } = render(AnxRefPreview);
      component.open(model("card:initiative-plans"), anchor());
      await Promise.resolve();

      // The chip asks to close as the pointer leaves it.
      component.requestClose();
      // Partway there the card is still on screen, which is the whole point:
      // closing immediately made Open and Copy ref impossible to reach.
      vi.advanceTimersByTime(60);
      await Promise.resolve();
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("stays open once the pointer arrives on the card", async () => {
    vi.useFakeTimers();
    try {
      const { container, component } = render(AnxRefPreview);
      component.open(model("card:initiative-plans"), anchor());
      await Promise.resolve();
      component.requestClose();

      const card = container.querySelector(".anx-ref-preview");
      await fireEvent.mouseEnter(card);
      vi.advanceTimersByTime(1000);
      await Promise.resolve();
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("closes once the pointer leaves without arriving", async () => {
    vi.useFakeTimers();
    try {
      const { container, component } = render(AnxRefPreview);
      component.open(model("card:initiative-plans"), anchor());
      await Promise.resolve();
      component.requestClose();
      vi.advanceTimersByTime(1000);
      await Promise.resolve();
      await waitFor(() =>
        expect(container.querySelector(".anx-ref-preview")).toBeNull(),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("closes on Escape and hands focus back to the chip", async () => {
    const { container, component } = render(AnxRefPreview);
    const chip = anchor();
    chip.tabIndex = 0;
    component.open(model("card:initiative-plans"), chip);
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).not.toBeNull(),
    );

    // Focus a control inside the card, as Tab would.
    const copy = container.querySelector(".anx-ref-preview__actions button");
    copy.focus();
    expect(document.activeElement).toBe(copy);

    await fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() =>
      expect(container.querySelector(".anx-ref-preview")).toBeNull(),
    );
    expect(document.activeElement).toBe(chip);
  });
});
