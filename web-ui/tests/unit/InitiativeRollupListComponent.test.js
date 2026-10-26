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
});
