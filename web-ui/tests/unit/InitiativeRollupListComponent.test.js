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
import { rollupRows } from "../../src/lib/watchRollup.js";
import { mockupBrief } from "../fixtures/watchMockupSeed.js";

const NOW = Date.parse("2026-10-24T12:00:00Z");
const rows = rollupRows(
  normalizeBrief(mockupBrief("2026-10-24T12:00:00Z")).initiatives,
);
const props = {
  rows,
  now: NOW,
  hrefFor: (ref) => `/i/${ref}`,
  decideHref: (row) => `/decide?initiative=${row.ref}`,
};
const rowEl = (c, ref) => c.querySelector(`[data-collection-row="${ref}"]`);

beforeEach(() => {
  cache.readWorkSnapshot.mockReset().mockReturnValue(null);
  cache.prefetchWork.mockReset().mockReturnValue(new Promise(() => {}));
});
afterEach(cleanup);

describe("InitiativeRollupList", () => {
  it("opens rows that need the reader, without reading their cards", () => {
    const { container } = render(InitiativeRollupList, props);
    expect(rowEl(container, "card:capture").dataset.expanded).toBe("true");
    expect(
      rowEl(container, "card:capture").querySelectorAll("[data-rollup-kid]"),
    ).toHaveLength(3);
    expect(rowEl(container, "card:smarter").dataset.expanded).toBe("false");
    expect(cache.prefetchWork).not.toHaveBeenCalled();
    // The plan waits for a tap.
    expect(
      rowEl(container, "card:capture").querySelector("[data-details-load]"),
    ).not.toBeNull();
  });

  it("reads a row's plan once the reader opens it", async () => {
    const { container } = render(InitiativeRollupList, props);
    const smarter = rowEl(container, "card:smarter");
    await fireEvent.click(smarter.querySelector(".grid-row"));
    expect(smarter.dataset.expanded).toBe("true");
    expect(cache.prefetchWork).toHaveBeenCalledTimes(1);
    expect(cache.prefetchWork.mock.calls[0][0]).toBe("card:smarter");
    await fireEvent.click(smarter.querySelector("[data-collection-toggle]"));
    expect(smarter.dataset.expanded).toBe("false");
  });

  it("closes a default-open row and keeps it closed", async () => {
    const { container } = render(InitiativeRollupList, props);
    const toggle = rowEl(container, "card:capture").querySelector(
      "[data-collection-toggle]",
    );
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    await fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(
      rowEl(container, "card:capture").querySelector("[data-rollup-kid]"),
    ).toBeNull();
  });

  it("filters by a chip and folds done away", async () => {
    const { container } = render(InitiativeRollupList, props);
    const chips = [...container.querySelectorAll("[data-collection-filter]")];
    expect(chips.map((c) => c.textContent.trim())).toEqual([
      "3 need you",
      "1 slipping",
      "1 moving",
    ]);
    expect(rowEl(container, "card:docs-site")).toBeNull();
    await fireEvent.click(chips[1]);
    expect(
      [...container.querySelectorAll("[data-collection-row]")].map(
        (r) => r.dataset.collectionRow,
      ),
    ).toEqual(["card:retention"]);
  });

  it("draws workstream kid rows from their own rollups", () => {
    const { container } = render(InitiativeRollupList, props);
    const kid = rowEl(container, "card:capture").querySelector(
      '[data-rollup-kid="card:record-lane"]',
    );
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
    // Owners come from the brief; without a directory, the ref's name.
    expect(kid.querySelector("[data-kid-owner]").textContent.trim()).not.toBe(
      "—",
    );
    const speaker = rowEl(container, "card:capture").querySelector(
      '[data-rollup-kid="card:speaker-id"] [data-kid-promise]',
    );
    expect(speaker.textContent).toBe("10h past");
    expect(speaker.className).toContain("text-warn-text");
  });

  it("shows the outcome and a proposed tag", () => {
    const { container } = render(InitiativeRollupList, props);
    const smarter = rowEl(container, "card:smarter");
    expect(smarter.dataset.proposed).toBe("true");
    expect(
      smarter.querySelector("[data-rollup-proposed]").textContent.trim(),
    ).toBe("proposed");
    expect(
      smarter.querySelector("[data-rollup-outcome]").textContent.trim(),
    ).toBe("proactive help users act on · readouts per experiment");
    expect(
      rowEl(container, "card:capture").querySelector("[data-rollup-proposed]"),
    ).toBeNull();
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

    it("counts initiatives against the target in the header, quietly", () => {
      const { container } = render(
        InitiativeRollupList,
        setProps({
          initiative_budget: { count: 7, target: 15, truncated: false },
        }),
      );
      const chip = container.querySelector("[data-initiative-budget]");
      expect(chip.textContent.trim()).toBe("Initiatives: 7 of 15");
      // The exact count reads as text; an aria-label on a generic span is
      // not announced.
      expect(chip.getAttribute("aria-label")).toBeNull();
      expect(chip.querySelector(".sr-only")).toBeNull();
      expect(container.querySelector("[data-over-target]")).toBeNull();
      expect(container.querySelector("[data-initiative-changes]")).toBeNull();
    });

    it("states the facts over target and links only an open ask", async () => {
      const brief = {
        initiative_budget: { count: 17, target: 15, truncated: true },
        over_target: {
          count: 17,
          target: 15,
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
      expect(line.textContent.replace(/\s+/g, " ").trim()).toBe(
        "Over target At least 17 initiatives against a target of 15.",
      );
      // The truncated "+" is a sighted lower bound; the announced name is
      // the full sentence plus the out-of-view note, as text.
      const chip = container.querySelector("[data-initiative-budget]");
      expect(chip.querySelector('[aria-hidden="true"]').textContent).toBe(
        "Initiatives: 17+ of 15",
      );
      expect(
        chip.querySelector(".sr-only").textContent.replace(/\s+/g, " ").trim(),
      ).toBe(
        "At least 17 initiatives against a target of 15. Some initiatives may be outside your view.",
      );
      expect(container.querySelector("[data-over-target-ask]")).toBeNull();
      expect(line.querySelector("button")).toBeNull();
      await rerender({ ...setProps(brief), overTargetHref: "/decide/ask-7" });
      expect(
        container.querySelector("[data-over-target-ask]").getAttribute("href"),
      ).toBe("/decide/ask-7");
    });

    it("lists recent changes open, with links, reasons and a truncation note", async () => {
      const items = Array.from({ length: 7 }, (_, i) =>
        changeRow(i + 1, "2026-10-24T09:00:00Z"),
      );
      const { container } = render(
        InitiativeRollupList,
        setProps({ initiative_changes: { items, truncated: true } }),
      );
      const strip = container.querySelector("[data-initiative-changes]");
      const toggle = strip.querySelector("[data-initiative-changes-toggle]");
      expect(toggle.getAttribute("aria-expanded")).toBe("true");
      expect(toggle.textContent).toContain("(7+)");
      const rowsShown = strip.querySelectorAll("[data-initiative-change]");
      expect(rowsShown).toHaveLength(5);
      expect(rowsShown[0].textContent.replace(/\s+/g, " ").trim()).toBe(
        "Added Card 1 — by codex: Independent outcome,·3h ago",
      );
      expect(rowsShown[1].textContent.replace(/\s+/g, " ").trim()).toBe(
        "Added Card 2 — by codex,·3h ago",
      );
      expect(rowsShown[0].querySelector("a").getAttribute("href")).toBe(
        "/i/card:c1",
      );
      expect(
        strip.querySelector("[data-initiative-changes-truncated]"),
      ).not.toBeNull();
      await fireEvent.click(
        strip.querySelector("[data-initiative-changes-more]"),
      );
      expect(strip.querySelectorAll("[data-initiative-change]")).toHaveLength(
        7,
      );
    });

    it("starts closed when the newest change is over a week old", async () => {
      const { container } = render(
        InitiativeRollupList,
        setProps({
          initiative_changes: {
            items: [changeRow(1, "2026-10-10T09:00:00Z", "initiative_retired")],
            truncated: false,
          },
        }),
      );
      const strip = container.querySelector("[data-initiative-changes]");
      expect(strip.dataset.open).toBe("false");
      expect(strip.querySelector("[data-initiative-change]")).toBeNull();
      await fireEvent.click(
        strip.querySelector("[data-initiative-changes-toggle]"),
      );
      expect(
        strip.querySelector("[data-initiative-change]").textContent,
      ).toMatch(/^\s*Retired/);
    });

    it("tags the new pieces with brief fields (I1)", () => {
      const { container } = render(
        InitiativeRollupList,
        setProps({
          initiative_budget: { count: 17, target: 15, truncated: false },
          over_target: {
            count: 17,
            target: 15,
            truncated: false,
            next_actions: [],
          },
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
