// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
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
import FyiList from "../../src/lib/components/watch/FyiList.svelte";
import UpkeepNotice from "../../src/lib/components/watch/UpkeepNotice.svelte";
import { normalizeBrief } from "../../src/lib/brief.js";
import { watchRows } from "../../src/lib/watchList.js";
import { lintComputed } from "./briefLint.js";
import fixture from "../../../contracts/fixtures/brief/brief.v1.json";

const NOW = Date.parse("2026-10-15T09:00:00Z");
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();

/** One initiative in the fixture's shape, with the given overrides. */
function initiative(overrides) {
  return {
    ...fixture.initiatives.find((row) => row.ref === "card:observability"),
    ...overrides,
  };
}
function list(initiatives, extra = {}) {
  const brief = normalizeBrief({ ...fixture, initiatives });
  return render(InitiativeRollupList, {
    rows: watchRows(brief.initiatives, NOW),
    now: NOW,
    hrefFor: (ref) => `/i/${ref}`,
    decideHref: (row) => `/decide?initiative=${row.ref}`,
    ...extra,
  });
}
const battery = fixture.initiatives.find(
  (row) => row.ref === "card:battery-measurement",
).measure;

beforeEach(() => {
  cache.readWorkSnapshot.mockReset().mockReturnValue(null);
  cache.prefetchWork.mockReset().mockReturnValue(new Promise(() => {}));
});
afterEach(cleanup);

describe("a Watch row with a measure", () => {
  it("leads with the reading, the change and a sparkline", () => {
    const { container } = list([
      initiative({ ref: "card:battery", title: "Battery", measure: battery }),
    ]);
    const line = container.querySelector("[data-rollup-measure]");
    expect(text(line)).toBe(
      "Battery life per charge 41.5h → 48h up 2.3 in 7d read 1d ago",
    );
    expect(line.dataset.measureReading).toBe("true");
    expect(line.getAttribute("data-source")).toBe(
      "computed:initiatives.measure",
    );
    expect(line.querySelector("[data-measure-change]").className).toContain(
      "text-ok-text",
    );
    expect(line.querySelector("[data-measure-sparkline] polyline")).not.toBe(
      null,
    );
    // The measure comes before the next-step line.
    const main = line.parentElement;
    expect([...main.children].indexOf(line)).toBeLessThan(
      [...main.children].indexOf(main.querySelector("[data-rollup-next]")),
    );
    expect(lintComputed(container)).toEqual([]);
  });

  it("says No reading in the warning tone, with the target", () => {
    const { container } = list([
      initiative({
        ref: "card:hosts",
        title: "Hosts",
        measure: fixture.initiatives.find(
          (row) => row.ref === "card:fleet-onboarding",
        ).measure,
      }),
    ]);
    const line = container.querySelector("[data-rollup-measure]");
    expect(text(line)).toBe("Hosts enrolled No reading target 7");
    expect(line.dataset.measureReading).toBe("false");
    expect(line.querySelector("b").className).toContain("text-warn-text");
    expect(line.querySelector("[data-measure-sparkline]")).toBeNull();
  });

  it("shows a reading's age and a bad change in the danger tone", () => {
    const { container } = list([
      initiative({
        ref: "card:churn",
        title: "Churn",
        measure: {
          ...battery,
          label: "Monthly churn",
          direction: "down",
          target: 10,
          unit: "%",
          latest: 13.1,
          previous: 12.4,
          age_seconds: 4 * 86_400,
        },
      }),
    ]);
    const line = container.querySelector("[data-rollup-measure]");
    expect(text(line)).toBe(
      "Monthly churn 13.1% → 10% up 0.7 in 7d read 4d ago",
    );
    expect(line.querySelector("[data-measure-change]").className).toContain(
      "text-danger-text",
    );
    expect(line.querySelector("[data-measure-stale]").className).toContain(
      "text-fg-muted",
    );
    expect(lintComputed(container)).toEqual([]);
  });

  it("demotes a late promise to a reporting note, only on measured rows", () => {
    const late = {
      state: "past_promise",
      past_promise_for_seconds: 3 * 3600,
      promise: { at: "2026-10-15T06:00:00Z", what: "ship it", source: "note" },
    };
    const { container } = list([
      initiative({
        ref: "card:a",
        title: "Measured",
        measure: battery,
        ...late,
      }),
      initiative({ ref: "card:b", title: "Plain", ...late }),
    ]);
    const next = (ref) =>
      text(
        container.querySelector(
          `[data-collection-row="${ref}"] [data-rollup-next]`,
        ),
      );
    expect(next("card:a")).toBe("reporting late 3h · next: ship it · Oct 15");
    expect(next("card:b")).toBe("own promise 3h past · next: ship it · Oct 15");
    expect(
      container.querySelector(
        '[data-collection-row="card:b"] [data-rollup-measure]',
      ),
    ).toBeNull();
  });
});

describe("FyiList", () => {
  const items = normalizeBrief(fixture).fyi;

  it("lists what agents told the reader, with how often it arrived", () => {
    const { container } = render(FyiList, {
      items,
      now: NOW,
      hrefFor: (ref) => `/i/${ref}`,
    });
    expect(text(container.querySelector("h2"))).toBe(
      "For your information · 1",
    );
    const row = container.querySelector('[data-fyi-row="event:fyi-7"]');
    expect(text(row)).toBe(
      "Nightly backlog digest: 3 cards past promise 3 of 41 open cards passed their promise overnight; their owners were asked to renew or close them. guard on host-2 · 6h ago · Observability · arrived 5 times",
    );
    expect(row.querySelector("a").getAttribute("href")).toBe(
      "/i/card:observability",
    );
    expect(lintComputed(container)).toEqual([]);
  });

  it("renders nothing when there is nothing to tell", () => {
    const { container } = render(FyiList, { items: [], now: NOW });
    expect(container.querySelector("[data-watch-fyi]")).toBeNull();
  });

  it("omits the repeat count and a body equal to the title", () => {
    const { container } = render(FyiList, {
      items: [{ ...items[0], repeats: 0, text: items[0].title }],
      now: NOW,
      hrefFor: () => "#",
    });
    expect(container.querySelector("[data-fyi-repeats]")).toBeNull();
    expect(container.querySelectorAll("[data-fyi-row] p")).toHaveLength(2);
  });
});

describe("UpkeepNotice", () => {
  it("says nobody is tending the workspace and names the fix", () => {
    const { container } = render(UpkeepNotice, {
      upkeep: normalizeBrief(fixture).upkeep,
    });
    const notice = container.querySelector("[data-upkeep-notice]");
    expect(text(notice)).toBe(
      "3 upkeep suggestions have waited more than 9 days. No agent is tending this workspace. Give one agent the project-manager skill: anx skills sync --pm",
    );
    expect(notice.querySelector("code").textContent).toBe(
      "anx skills sync --pm",
    );
    expect(lintComputed(container)).toEqual([]);
  });

  it("stays quiet while suggestions are fresh, and reads on the list", () => {
    const fresh = render(UpkeepNotice, {
      upkeep: {
        open: 3,
        truncated: false,
        oldestWaiting: 3600,
        untended: false,
      },
    });
    expect(fresh.container.querySelector("[data-upkeep-notice]")).toBeNull();
    cleanup();
    const brief = normalizeBrief(fixture);
    const { container } = list(fixture.initiatives, {
      fyi: brief.fyi,
      upkeep: brief.upkeep,
    });
    expect(container.querySelector("[data-upkeep-notice]")).not.toBeNull();
    expect(container.querySelector("[data-watch-fyi]")).not.toBeNull();
  });
});
