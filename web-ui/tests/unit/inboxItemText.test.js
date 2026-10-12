import { describe, expect, it } from "vitest";

import {
  INBOX_HEADING_MAX_CHARS,
  clampInboxHeading,
  inboxItemText,
  stripHandledInboxTitle,
  subjectTitleRepeatsAsk,
} from "../../src/lib/inboxItemText.js";

/** The shape the Inbox saw in dogfood: a whole ask filed as the title. */
const LEGACY_ASK_TITLE =
  "Should we ship the inbox rendering fix in 0.12.58 or hold it for the " +
  "next minor? Context: the heading today prints the entire ask in bold, " +
  "which readers called ugly and overwhelming. Options: ship now behind no " +
  "flag, hold for the next minor, or split the data contract out first. " +
  "Recommendation: ship now, because the renderer change is additive and " +
  "the data contract lands separately in core.";

describe("clampInboxHeading", () => {
  it("leaves a short title alone", () => {
    expect(clampInboxHeading("Approve the rollout window")).toEqual({
      heading: "Approve the rollout window",
      rest: "",
      truncated: false,
    });
  });

  it("treats the author's own line break as the heading boundary", () => {
    const split = clampInboxHeading(
      "Approve the rollout window\n\n## Options\n- Ship now\n- Hold",
    );
    expect(split.heading).toBe("Approve the rollout window");
    expect(split.rest).toBe("## Options\n- Ship now\n- Hold");
    expect(split.truncated).toBe(false);
  });

  it("drops a markdown heading marker from the first line", () => {
    expect(clampInboxHeading("## Decision needed\nBody prose").heading).toBe(
      "Decision needed",
    );
  });

  it("cuts a long title at the last sentence end inside the budget", () => {
    const split = clampInboxHeading(LEGACY_ASK_TITLE);
    expect(split.heading).toBe(
      "Should we ship the inbox rendering fix in 0.12.58 or hold it for the " +
        "next minor?",
    );
    expect(split.heading.length).toBeLessThanOrEqual(INBOX_HEADING_MAX_CHARS);
    expect(split.truncated).toBe(false);
    expect(split.rest).toMatch(/^Context: the heading today/);
    // Nothing is dropped: heading plus rest is the title again.
    expect(`${split.heading} ${split.rest}`).toBe(LEGACY_ASK_TITLE);
  });

  it("does not end a heading on a version number or a decimal", () => {
    expect(clampInboxHeading("Ship 0.12.58 today").heading).toBe(
      "Ship 0.12.58 today",
    );
  });

  it("keeps a title barely over budget whole rather than orphaning a word", () => {
    const title = `Escalation ${"f".repeat(64)} must be answered before the rollout window closes`;
    expect(title.length).toBeGreaterThan(INBOX_HEADING_MAX_CHARS);
    const split = clampInboxHeading(title);
    expect(split.heading).toBe(title);
    expect(split.rest).toBe("");
    expect(split.truncated).toBe(false);
  });

  it("falls back to a word boundary when the first sentence is too long", () => {
    const title = `${"word ".repeat(60).trim()}. tail`;
    const split = clampInboxHeading(title);
    expect(split.truncated).toBe(true);
    expect(split.heading.endsWith("…")).toBe(true);
    expect(split.heading.length).toBeLessThanOrEqual(
      INBOX_HEADING_MAX_CHARS + 1,
    );
    // The cut lands on a space, so no word is broken in half.
    expect(split.heading.slice(0, -1)).toBe(
      title.slice(0, split.heading.length - 1).trimEnd(),
    );
    expect(split.rest.startsWith("word")).toBe(true);
  });

  it("ignores a sentence end that would leave an acknowledgement as the heading", () => {
    const split = clampInboxHeading(`Quick one. ${"detail ".repeat(40)}`);
    expect(split.heading).not.toBe("Quick one.");
    expect(split.truncated).toBe(true);
  });

  it("cuts mid-word only when the title has no space to cut on", () => {
    const blob = "a".repeat(400);
    const split = clampInboxHeading(blob);
    expect(split.heading).toBe(`${"a".repeat(INBOX_HEADING_MAX_CHARS)}…`);
    expect(split.rest).toBe("a".repeat(400 - INBOX_HEADING_MAX_CHARS));
  });

  it("honours an explicit budget", () => {
    const split = clampInboxHeading("one two three four five", {
      maxChars: 10,
    });
    expect(split.heading).toBe("one two…");
    expect(split.rest).toBe("three four five");
  });

  it("returns empty for a missing title", () => {
    expect(clampInboxHeading(undefined)).toEqual({
      heading: "",
      rest: "",
      truncated: false,
    });
    expect(clampInboxHeading("   \n  ").heading).toBe("");
  });
});

describe("inboxItemText", () => {
  it("keeps a well-formed ask exactly as written", () => {
    const text = inboxItemText({
      title: "Ship the inbox rendering fix in 0.12.58?",
      body: "## Decision\nShip or hold.\n\n## Options\n- Ship now\n- Hold",
    });
    expect(text.heading).toBe("Ship the inbox rendering fix in 0.12.58?");
    expect(text.body).toBe(
      "## Decision\nShip or hold.\n\n## Options\n- Ship now\n- Hold",
    );
    expect(text.movedFromTitle).toBe(false);
  });

  it("moves the rest of a long title into the body", () => {
    const text = inboxItemText({ title: LEGACY_ASK_TITLE, body: "" });
    expect(text.heading.length).toBeLessThanOrEqual(INBOX_HEADING_MAX_CHARS);
    expect(text.movedFromTitle).toBe(true);
    expect(text.body).toMatch(/^Context: the heading today/);
    expect(text.body).toMatch(/Recommendation: ship now/);
  });

  it("puts what moved out of the title above the body the item already had", () => {
    const text = inboxItemText({
      title: LEGACY_ASK_TITLE,
      body: "## Evidence\n- card:anx-742",
    });
    expect(text.body.startsWith("Context: the heading today")).toBe(true);
    expect(text.body.endsWith("## Evidence\n- card:anx-742")).toBe(true);
  });

  it("offers the whole title, on one line, for hover", () => {
    const text = inboxItemText({ title: "Approve\n  the   rollout" });
    expect(text.fullTitle).toBe("Approve the rollout");
  });

  it("survives an item with neither a title nor a body", () => {
    expect(inboxItemText({})).toEqual({
      heading: "",
      body: "",
      fullTitle: "",
      movedFromTitle: false,
      truncated: false,
    });
  });

  it("drops core's handled prefix before splitting the title", () => {
    const text = inboxItemText({
      title: `Human response recorded: ${LEGACY_ASK_TITLE}`,
      body: "",
    });
    const unprefixed = inboxItemText({ title: LEGACY_ASK_TITLE, body: "" });
    expect(text.heading).toBe(unprefixed.heading);
    expect(text.body).toBe(unprefixed.body);
    expect(text.fullTitle).toBe(unprefixed.fullTitle);
    expect(text.heading).not.toMatch(/Human response recorded/i);
  });
});

describe("subjectTitleRepeatsAsk", () => {
  it("catches the card `anx ask` titled with the ask itself", () => {
    expect(subjectTitleRepeatsAsk(LEGACY_ASK_TITLE, LEGACY_ASK_TITLE)).toBe(
      true,
    );
  });

  it("ignores whitespace, case and a trailing ellipsis from a clipped title", () => {
    expect(
      subjectTitleRepeatsAsk(
        `${LEGACY_ASK_TITLE.slice(0, 180)}…`,
        LEGACY_ASK_TITLE,
      ),
    ).toBe(true);
    expect(
      subjectTitleRepeatsAsk(
        "  approve the rollout window  ",
        "Approve the rollout window.",
      ),
    ).toBe(true);
  });

  it("keeps a card title that opens with the ask and then says more", () => {
    // The suffix is the card saying something the ask does not. Replacing the
    // line with "Card created for this ask" would hide it.
    expect(
      subjectTitleRepeatsAsk(
        `${LEGACY_ASK_TITLE} Rollout owner is Dana.`,
        LEGACY_ASK_TITLE,
      ),
    ).toBe(false);
    expect(
      subjectTitleRepeatsAsk(
        "Approve the rollout window — blocked on the legal signer",
        "Approve the rollout window",
      ),
    ).toBe(false);
  });

  it("does not call a short shared opening a repeat", () => {
    expect(
      subjectTitleRepeatsAsk(
        "Fix login",
        "Fix login before the rollout window closes, or push the window?",
      ),
    ).toBe(false);
  });

  it("leaves an ordinary card title alone", () => {
    expect(subjectTitleRepeatsAsk("Inbox rendering", LEGACY_ASK_TITLE)).toBe(
      false,
    );
  });

  it("is false when either side is missing", () => {
    expect(subjectTitleRepeatsAsk("", LEGACY_ASK_TITLE)).toBe(false);
    expect(subjectTitleRepeatsAsk(LEGACY_ASK_TITLE, "")).toBe(false);
  });

  it("still matches when core prefixed the handled ask", () => {
    expect(
      subjectTitleRepeatsAsk(
        LEGACY_ASK_TITLE,
        `Human response recorded: ${LEGACY_ASK_TITLE}`,
      ),
    ).toBe(true);
  });
});

describe("stripHandledInboxTitle", () => {
  it("drops core's handled prefix and leaves other titles alone", () => {
    expect(
      stripHandledInboxTitle("Human response recorded: Confirm quest"),
    ).toBe("Confirm quest");
    expect(
      stripHandledInboxTitle("Human response recorded Confirm quest"),
    ).toBe("Confirm quest");
    expect(stripHandledInboxTitle("Approve the rollout window")).toBe(
      "Approve the rollout window",
    );
  });
});
