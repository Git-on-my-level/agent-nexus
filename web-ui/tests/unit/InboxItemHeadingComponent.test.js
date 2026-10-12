// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import InboxItemHeading from "../../src/lib/components/inbox/InboxItemHeading.svelte";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

/**
 * jsdom lays nothing out, so clipping has to be stated rather than measured.
 * `scrollHeight` over `clientHeight` is exactly what the component reads.
 */
function stubClipped(extraHeight) {
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(40);
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(
    40 + extraHeight,
  );
}

describe("InboxItemHeading", () => {
  it("renders the heading at the requested level and clamps it to two lines", () => {
    const { container } = render(InboxItemHeading, {
      text: "Ship the inbox rendering fix in 0.12.58?",
      full: "Ship the inbox rendering fix in 0.12.58?",
      level: 1,
    });
    const heading = container.querySelector("[data-inbox-heading]");
    expect(heading?.tagName).toBe("H1");
    expect(heading?.textContent?.trim()).toBe(
      "Ship the inbox rendering fix in 0.12.58?",
    );
    expect(heading?.className).toContain("line-clamp-2");
  });

  it("offers the whole title on hover, and nothing when it is already shown", () => {
    const { container, unmount } = render(InboxItemHeading, {
      text: "Should we ship now?",
      full: "Should we ship now? Context: the heading prints the whole ask.",
      level: 2,
    });
    const heading = container.querySelector("[data-inbox-heading]");
    expect(heading?.tagName).toBe("H2");
    expect(heading?.getAttribute("title")).toBe(
      "Should we ship now? Context: the heading prints the whole ask.",
    );
    unmount();

    const same = render(InboxItemHeading, {
      text: "Should we ship now?",
      full: "Should we ship now?",
    });
    expect(
      same.container
        .querySelector("[data-inbox-heading]")
        ?.hasAttribute("title"),
    ).toBe(false);
  });

  it("renders nothing for an empty heading", () => {
    const { container } = render(InboxItemHeading, { text: "   " });
    expect(container.querySelector("[data-inbox-heading]")).toBeNull();
  });

  it("offers no expand control while the heading fits", () => {
    stubClipped(0);
    const { container } = render(InboxItemHeading, {
      text: "Approve the rollout window",
    });
    expect(container.querySelector("[data-inbox-heading-expand]")).toBeNull();
  });

  it("expands a clipped heading and folds it back", async () => {
    stubClipped(30);
    const { container } = render(InboxItemHeading, {
      text: "Should we ship the inbox rendering fix in 0.12.58 or hold it?",
    });
    const expand = container.querySelector("[data-inbox-heading-expand]");
    expect(expand?.textContent?.trim()).toBe("Show full title");
    expect(expand?.getAttribute("aria-expanded")).toBe("false");

    expand.click();
    await Promise.resolve();
    const heading = container.querySelector("[data-inbox-heading]");
    expect(heading?.className).not.toContain("line-clamp-2");
    expect(heading?.getAttribute("data-inbox-heading-expanded")).toBe("true");
    // The control that expanded it stays, so the reader can fold it back.
    const fold = container.querySelector("[data-inbox-heading-expand]");
    expect(fold?.textContent?.trim()).toBe("Show less");

    fold.click();
    await Promise.resolve();
    expect(
      container.querySelector("[data-inbox-heading]")?.className,
    ).toContain("line-clamp-2");
  });

  it("folds again when the heading text changes after an expand", async () => {
    stubClipped(30);
    const view = render(InboxItemHeading, {
      text: "Should we ship the inbox rendering fix in 0.12.58 or hold it?",
    });
    view.container.querySelector("[data-inbox-heading-expand]").click();
    await Promise.resolve();
    expect(
      view.container.querySelector("[data-inbox-heading]")?.className,
    ).not.toContain("line-clamp-2");

    stubClipped(0);
    await view.rerender({ text: "Approve the rollout window" });
    await Promise.resolve();
    expect(
      view.container.querySelector("[data-inbox-heading]")?.className,
    ).toContain("line-clamp-2");
    expect(
      view.container.querySelector("[data-inbox-heading-expand]"),
    ).toBeNull();
  });
});
