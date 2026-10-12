// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import InboxContextStrip from "../../src/lib/components/inbox/InboxContextStrip.svelte";

afterEach(() => cleanup());

const ASK =
  "Should we ship the inbox rendering fix in 0.12.58 or hold it for the " +
  "next minor? Context: the heading today prints the entire ask in bold. " +
  "Recommendation: ship now.";

const strip = (props) =>
  render(InboxContextStrip, {
    relation: "Blocks",
    subjectHref: "/o/local/w/local/tasks/card:anx-742",
    ...props,
  });

describe("InboxContextStrip duplication", () => {
  it("names the card it blocks when the card has a title of its own", () => {
    const { container } = strip({
      subject: { title: "Inbox rendering", status: "in_progress" },
      askText: ASK,
    });
    const line = container.querySelector("[data-inbox-context-subject]");
    expect(line?.textContent).toContain("Blocks");
    expect(line?.textContent).toContain("Inbox rendering");
    expect(line?.hasAttribute("data-inbox-context-repeats-ask")).toBe(false);
  });

  it("does not print the ask a second time when the card is titled with it", () => {
    const { container } = strip({
      subject: { title: ASK, status: "in_progress" },
      askText: ASK,
    });
    const line = container.querySelector("[data-inbox-context-subject]");
    expect(line?.getAttribute("data-inbox-context-repeats-ask")).toBe("true");
    expect(line?.textContent).toContain("Card created for this ask");
    expect(line?.textContent).not.toContain("Should we ship the inbox");
    // The relation word would read "Blocks Card created for this ask".
    expect(line?.textContent).not.toContain("Blocks");
    // The status still shows; it is the half of this box that is not the ask.
    expect(line?.textContent).toContain("in_progress");
    // And it is still a link to the card.
    expect(line?.querySelector("a")?.getAttribute("href")).toBe(
      "/o/local/w/local/tasks/card:anx-742",
    );
  });

  it("still hides the card title when core prefixed a handled ask", () => {
    const { container } = strip({
      subject: { title: ASK },
      askText: `Human response recorded: ${ASK}`,
    });
    expect(
      container
        .querySelector("[data-inbox-context-subject]")
        ?.getAttribute("data-inbox-context-repeats-ask"),
    ).toBe("true");
    expect(
      container.querySelector("[data-inbox-context-subject]")?.textContent,
    ).toContain("Card created for this ask");
  });

  it("catches a card title that is the ask clipped short", () => {
    const { container } = strip({
      subject: { title: `${ASK.slice(0, 120)}…` },
      askText: ASK,
    });
    expect(
      container
        .querySelector("[data-inbox-context-subject]")
        ?.textContent?.trim(),
    ).toBe("Card created for this ask");
  });

  it("renders the card title as plain text when the card has no page", () => {
    const { container } = strip({
      subject: { title: ASK },
      askText: ASK,
      subjectHref: "",
    });
    const line = container.querySelector("[data-inbox-context-subject]");
    expect(line?.querySelector("a")).toBeNull();
    expect(line?.textContent?.trim()).toBe("Card created for this ask");
  });
});
