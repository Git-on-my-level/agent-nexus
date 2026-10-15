// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import { readable } from "svelte/store";

vi.mock("$app/stores", () => ({
  page: readable({
    url: new URL("http://localhost/o/acme/w/main/watch"),
    params: { organization: "acme", workspace: "main" },
  }),
}));

import WatchRow from "../../src/lib/components/watch/WatchRow.svelte";
import { BRIEF_COMPUTED_FIELDS } from "../../src/lib/brief.js";

const NOW = Date.parse("2026-10-15T09:00:00Z");
const row = (extra = {}) => ({
  ref: "card:battery",
  id: "battery",
  title: "Battery measurement",
  state: "past_promise",
  placement: "in_progress",
  promise: {
    at: "2026-10-06T00:00:00Z",
    what: "checkpoint",
    source: "plan_step",
  },
  pastPromiseFor: 9 * 86400,
  promiseMoves: 2,
  lastEvidenceAt: "2026-10-04T12:00:00Z",
  progress: { done: 6, total: 9 },
  blockedCount: 1,
  waitingOnYouCount: 2,
  note: {
    text: "Waiting on the rig",
    author: { id: "a", display: "Ada" },
    at: "2026-10-15T06:00:00Z",
  },
  possiblyMoot: { reason: "Superseded" },
  ...extra,
});

afterEach(cleanup);

describe("WatchRow", () => {
  for (const variant of ["row", "card"]) {
    it(`tags every computed piece (${variant})`, () => {
      const { container } = render(WatchRow, {
        initiative: row(),
        variant,
        now: NOW,
      });
      const sources = [...container.querySelectorAll("[data-source]")].map(
        (el) => el.getAttribute("data-source"),
      );
      for (const source of sources) {
        expect(source.startsWith("computed:")).toBe(true);
        expect(BRIEF_COMPUTED_FIELDS.has(source.slice(9))).toBe(true);
      }
      for (const field of [
        "initiatives.progress",
        "initiatives.blocked_count",
        "initiatives.waiting_on_you_count",
        "initiatives.promise",
        "initiatives.past_promise_for",
        "initiatives.promise_moves",
      ])
        expect(sources).toContain(`computed:${field}`);
      expect(
        container.querySelector('[data-source="computed:initiatives.progress"]')
          .textContent,
      ).toBe("6/9");
    });
  }

  it("links the title and badges, and quotes the note with its author", () => {
    const { container } = render(WatchRow, { initiative: row(), now: NOW });
    expect(
      container.querySelector("[data-watch-title]").getAttribute("href"),
    ).toContain("/initiatives/battery");
    const badge = container.querySelector(
      '[data-source="computed:initiatives.blocked_count"]',
    );
    expect(badge.getAttribute("href")).toContain(
      "/decide?initiative=card%3Abattery",
    );
    expect(badge.textContent).toContain("1");
    const chip = container.querySelector("[data-author-chip]");
    expect(chip.textContent).toContain("Ada");
    expect(chip.textContent).toContain("3h ago");
    expect(
      container.querySelector("[title='Superseded']").textContent,
    ).toContain("possibly moot");
  });

  it("omits badges, note and chip when there is nothing to show", () => {
    const { container } = render(WatchRow, {
      initiative: row({
        blockedCount: 0,
        waitingOnYouCount: 0,
        note: null,
        possiblyMoot: null,
        progress: null,
      }),
      now: NOW,
    });
    expect(container.querySelector("[data-author-chip]")).toBeNull();
    expect(
      container.querySelector(
        '[data-source="computed:initiatives.blocked_count"]',
      ),
    ).toBeNull();
  });

  it("renders the note as markdown, never raw asterisks", () => {
    const { container } = render(WatchRow, {
      initiative: row({
        note: {
          text: "**Goal:** land the `ask` fix",
          author: { id: "a", display: "Ada" },
          at: "2026-10-15T06:00:00Z",
        },
      }),
      now: NOW,
    });
    const note = container.querySelector("[data-watch-note]");
    expect(note.textContent).not.toContain("**");
    expect(note.querySelector("strong")?.textContent).toBe("Goal:");
    expect(note.querySelector("code")?.textContent).toBe("ask");
  });

  for (const variant of ["row", "card"]) {
    it(`draws a progress bar with its count (${variant})`, () => {
      const { container } = render(WatchRow, {
        initiative: row(),
        variant,
        now: NOW,
      });
      const bar = container.querySelector("[data-watch-progress]");
      expect(bar.getAttribute("role")).toBe("img");
      expect(bar.getAttribute("aria-label")).toContain("6 of 9 steps done");
      expect(bar.getAttribute("data-watch-progress")).toBe("late");
      expect(bar.querySelectorAll(".seg--done")).toHaveLength(6);
    });
  }
});
