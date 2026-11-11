// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { readable } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const cache = vi.hoisted(() => ({
  readWorkSnapshot: vi.fn(),
  prefetchWork: vi.fn(),
}));
vi.mock("$app/stores", () => ({
  page: readable({
    url: new URL("http://localhost/o/acme/w/main/watch"),
    params: { organization: "acme", workspace: "main" },
  }),
}));
vi.mock("$lib/coreClient", () => ({ coreClient: { getWork: vi.fn() } }));
vi.mock("$lib/workCache.js", () => cache);

import InitiativeRollupList from "../../src/lib/components/watch/InitiativeRollupList.svelte";
import { BRIEF_COMPUTED_FIELDS, normalizeBrief } from "../../src/lib/brief.js";
import { watchRows } from "../../src/lib/watchList.js";
import { mockupBrief } from "../fixtures/watchMockupSeed.js";

const NOW = Date.parse("2026-10-24T12:00:00Z");
const rows = watchRows(
  normalizeBrief(mockupBrief("2026-10-24T12:00:00Z")).initiatives,
  NOW,
);
const props = {
  rows,
  now: NOW,
  hrefFor: (ref) => `/i/${ref}`,
  decideHref: (row) => `/decide?initiative=${row.ref}`,
};
const rowEl = (c, ref) => c.querySelector(`[data-collection-row="${ref}"]`);
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();

beforeEach(() => {
  cache.readWorkSnapshot.mockReset().mockReturnValue(null);
  cache.prefetchWork.mockReset().mockReturnValue(new Promise(() => {}));
});
afterEach(cleanup);

describe("InitiativeRollupList", () => {
  it("lists sections in reading order with done folded", async () => {
    const { container } = render(InitiativeRollupList, props);
    expect(
      [...container.querySelectorAll("[data-watch-section] h2")].map(text),
    ).toEqual([
      "Needs you · 3",
      "Lagging · 1",
      "Moving · 1",
      "Not started · 1",
      "Done · 1 Show",
    ]);
    expect(rowEl(container, "card:docs-site")).toBeNull();
    // No filter chips or counter tiles on Watch.
    expect(container.querySelector("[data-collection-filter]")).toBeNull();
    await fireEvent.click(container.querySelector("[data-collection-folded]"));
    expect(rowEl(container, "card:docs-site").dataset.bucket).toBe("done");
  });

  it("starts every row closed, and reads a plan once opened", async () => {
    const { container } = render(InitiativeRollupList, props);
    for (const row of container.querySelectorAll("[data-collection-row]"))
      expect(row.dataset.expanded).toBe("false");
    expect(container.querySelector("[data-rollup-outcome]")).toBeNull();
    const smarter = rowEl(container, "card:smarter");
    await fireEvent.click(smarter.querySelector(".row"));
    expect(smarter.dataset.expanded).toBe("true");
    expect(cache.prefetchWork).toHaveBeenCalledTimes(1);
    expect(cache.prefetchWork.mock.calls[0][0]).toBe("card:smarter");
    // The outcome reads only once the row is open.
    expect(text(smarter.querySelector("[data-rollup-outcome]"))).toBe(
      "proactive help users act on · readouts per experiment",
    );
    await fireEvent.click(smarter.querySelector("[data-collection-toggle]"));
    expect(smarter.dataset.expanded).toBe("false");
  });

  it("shows one status line, a bar, the owner and why on the right", () => {
    const { container } = render(InitiativeRollupList, props);
    const capture = rowEl(container, "card:capture");
    expect(text(capture.querySelector("[data-rollup-next]"))).toBe(
      "own promise 10h past · 2 of 9 past · 2 blocked · next: legacy queues gone, no silent loss · Oct 24",
    );
    expect(
      capture
        .querySelector("[data-rollup-progress]")
        .getAttribute("aria-label"),
    ).toBe("1 of 9 children done");
    const status = capture.querySelector("[data-rollup-status]");
    expect(text(status)).toBe("Waiting on you");
    expect(status.getAttribute("href")).toBe("/decide?initiative=card:capture");
    expect(text(capture.querySelector("[data-rollup-children]"))).toBe(
      "9 children",
    );
    const retention = rowEl(container, "card:retention");
    expect(text(retention.querySelector("[data-rollup-status]"))).toBe(
      "1d past",
    );
    expect(text(retention.querySelector("[data-rollup-owner]"))).toContain(
      "Unassigned",
    );
  });

  it("draws workstream kid rows from their own rollups", async () => {
    const { container } = render(InitiativeRollupList, props);
    const capture = rowEl(container, "card:capture");
    await fireEvent.click(capture.querySelector("[data-collection-toggle]"));
    const kid = capture.querySelector('[data-rollup-kid="card:record-lane"]');
    expect(kid.querySelector("[data-kid-codes]").textContent).toBe(
      " · T1 T2 P86 P90",
    );
    expect(kid.querySelector("[data-kid-cards]").textContent).toBe(
      " · 9 cards",
    );
    expect(kid.querySelector("[data-kid-state]").textContent.trim()).toBe(
      "4 moving · 1 review",
    );
    expect(kid.querySelector("[data-kid-promise]").textContent).toBe(
      "due Oct 25",
    );
    const speaker = capture.querySelector(
      '[data-rollup-kid="card:speaker-id"] [data-kid-promise]',
    );
    expect(speaker.textContent).toBe("10h past");
    expect(speaker.className).toContain("text-warn-text");
  });

  it("tags every computed piece with a brief field (I1)", () => {
    const { container } = render(InitiativeRollupList, props);
    for (const el of container.querySelectorAll("[data-source]"))
      expect(BRIEF_COMPUTED_FIELDS).toContain(
        el.dataset.source.replace(/^computed:/, ""),
      );
  });

  describe("initiative set", () => {
    const changeRow = (n, ts, type = "initiative_promoted") => ({
      ref: `event:c${n}`,
      ts,
      type,
      actor_id: "codex",
      payload: {
        subject_ref: `card:c${n}`,
        title: `Card ${n}`,
        reason: n === 1 ? "Independent outcome" : "",
        before: { board_ref: "board:tasks", initiative: false },
        after: { board_ref: "board:initiatives", initiative: true },
      },
    });
    const setProps = (brief) => {
      const b = normalizeBrief(brief);
      return {
        ...props,
        budget: b.initiativeBudget,
        overTarget: b.overTarget,
        changes: b.initiativeChanges,
      };
    };

    it("says nothing about the target within it", () => {
      const { container } = render(
        InitiativeRollupList,
        setProps({
          initiative_budget: { count: 7, target: 15, truncated: false },
        }),
      );
      expect(container.querySelector("[data-over-target]")).toBeNull();
      expect(container.querySelector("[data-initiative-budget]")).toBeNull();
      expect(text(container.querySelector("[data-initiative-changes]"))).toBe(
        "Recent changes None in 7 days",
      );
    });

    it("counts open initiatives over target and links only an open ask", async () => {
      const brief = {
        initiative_budget: { count: 17, target: 5, truncated: true },
        over_target: {
          count: 17,
          target: 5,
          truncated: true,
          next_actions: [
            { command_id: "ask", argv: ["anx", "ask", "Fold or keep?"] },
          ],
        },
      };
      const { container, rerender } = render(
        InitiativeRollupList,
        setProps(brief),
      );
      const line = container.querySelector("[data-over-target]");
      // Six open rows on screen; done never counts and there is no "at least".
      expect(text(line)).toBe("Over target: 6 open against a target of 5.");
      expect(container.querySelector("[data-over-target-ask]")).toBeNull();
      expect(line.querySelector("button")).toBeNull();
      await rerender({ ...setProps(brief), overTargetHref: "/decide/ask-7" });
      expect(
        container.querySelector("[data-over-target-ask]").getAttribute("href"),
      ).toBe("/decide/ask-7");
    });

    it("keeps recent changes closed, last week only, with a count pill", async () => {
      const items = [
        ...Array.from({ length: 7 }, (_, i) =>
          changeRow(i + 1, "2026-10-24T09:00:00Z"),
        ),
        changeRow(8, "2026-10-10T09:00:00Z", "initiative_retired"),
      ];
      const { container } = render(
        InitiativeRollupList,
        setProps({ initiative_changes: { items, truncated: true } }),
      );
      const strip = container.querySelector("[data-initiative-changes]");
      const toggle = strip.querySelector("[data-initiative-changes-toggle]");
      expect(toggle.getAttribute("aria-expanded")).toBe("false");
      expect(text(strip.querySelector("[data-initiative-changes-count]"))).toBe(
        "7 in 7 days",
      );
      expect(strip.querySelector("[data-initiative-change]")).toBeNull();
      await fireEvent.click(toggle);
      const rowsShown = strip.querySelectorAll("[data-initiative-change]");
      expect(rowsShown).toHaveLength(5);
      expect(text(rowsShown[0])).toBe(
        "Added Card 1 — by codex: Independent outcome,·3h ago",
      );
      expect(rowsShown[0].querySelector("a").getAttribute("href")).toBe(
        "/i/card:c1",
      );
      // An older change is out of the window, so the list is not clipped.
      expect(
        strip.querySelector("[data-initiative-changes-truncated]"),
      ).toBeNull();
      await fireEvent.click(
        strip.querySelector("[data-initiative-changes-more]"),
      );
      expect(strip.querySelectorAll("[data-initiative-change]")).toHaveLength(
        7,
      );
      expect(strip.textContent).not.toContain("Card 8");
    });

    it("tags the new pieces with brief fields (I1)", () => {
      const { container } = render(
        InitiativeRollupList,
        setProps({
          initiative_budget: { count: 17, target: 5, truncated: false },
          initiative_changes: {
            items: [changeRow(1, "2026-10-24T09:00:00Z")],
            truncated: false,
          },
        }),
      );
      for (const el of container.querySelectorAll("[data-source]"))
        expect(BRIEF_COMPUTED_FIELDS).toContain(
          el.dataset.source.replace(/^computed:/, ""),
        );
    });
  });
});
