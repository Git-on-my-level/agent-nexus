// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import RefText from "../../src/lib/components/RefText.svelte";
import { reportRefStrings } from "../../src/lib/components/reports/reportRefs.js";
import { refResolveExample } from "../../src/lib/fixtures/refResolveExample.js";
import { indexResolvedRefs } from "../../src/lib/refResolve.js";

const resolved = indexResolvedRefs(refResolveExample);

const mount = (text) =>
  render(RefText, {
    text,
    resolved,
    organizationSlug: "scaling",
    workspaceSlug: "anx",
  });

afterEach(() => cleanup());

describe("RefText", () => {
  it("renders a ref inside prose as a chip and keeps the prose", () => {
    const { container } = mount("Blocked by card:pushed-series until Friday.");
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip).not.toBeNull();
    expect(chip.textContent).toContain("Pushed series and declared adapters");
    expect(container.textContent).toContain("Blocked by ");
    expect(container.textContent).toContain(" until Friday.");
  });

  it("links a bare URL that is not work", () => {
    const { container } = mount("See https://example.test/notes for detail.");
    const link = container.querySelector("a[href]");
    expect(link.getAttribute("href")).toBe("https://example.test/notes");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("renders a known pull request as a chip, not a bare link", () => {
    const { container } = mount(
      "fixed in https://github.com/Git-on-my-level/agent-nexus/pull/246",
    );
    expect(container.querySelector("[data-anx-ref]")).not.toBeNull();
    // The chip is the destination; no second anchor for the same URL.
    expect(container.querySelectorAll("a:not(.anx-ref-chip)")).toHaveLength(0);
  });

  it("renders markdown in report prose through the shared renderer", () => {
    const { container } = mount("**Goal:** ship `card:x` by Friday");
    expect(container.querySelector("strong")?.textContent).toBe("Goal:");
    // A ref inside a code span is an example, not a destination.
    expect(container.querySelector("code")?.textContent).toBe("card:x");
    expect(container.querySelector("[data-anx-ref]")).toBeNull();
  });

  it("renders an unresolvable ref as a not-found chip rather than dropping it", () => {
    const { container } = mount("Depends on card:deleted-thing.");
    const chip = container.querySelector("[data-anx-ref]");
    expect(chip.classList.contains("anx-ref-chip--missing")).toBe(true);
    expect(container.textContent).toContain("not found");
  });

  it("refuses a destination that would execute", () => {
    const { container } = mount("Try javascript:alert(1) now");
    expect(container.querySelector("a")).toBeNull();
    expect(container.textContent).toContain("javascript:alert(1)");
  });

  it("renders plain prose untouched", () => {
    const { container } = mount("Nothing to link here.");
    expect(container.querySelector("a")).toBeNull();
    expect(container.textContent).toBe("Nothing to link here.");
  });

  it("renders several refs in one string", () => {
    const { container } = mount(
      "card:pushed-series and doc:release-b-plan both move.",
    );
    expect(container.querySelectorAll("[data-anx-ref]")).toHaveLength(2);
  });
});

describe("reportRefStrings", () => {
  it("collects both table spellings, since the renderer chips both", () => {
    // A ref written only in a `table` was rendering "not found" because
    // collection asked for `evidence-table` alone.
    for (const type of ["table", "evidence-table"]) {
      expect(
        reportRefStrings([
          {
            type,
            data: {
              columns: ["Ref"],
              rows: [{ cells: ["card:initiative-plans"], source_ids: [] }],
            },
          },
        ]),
      ).toContain("card:initiative-plans");
    }
  });

  it("collects table cells, so a cell's refs resolve with the page", () => {
    const strings = reportRefStrings([
      {
        type: "evidence-table",
        data: {
          columns: ["Item", "Ref"],
          rows: [{ cells: ["Plan", "card:initiative-plans"], source_ids: [] }],
        },
      },
    ]);
    expect(strings).toContain("card:initiative-plans");
  });

  it("collects callout and explanation text", () => {
    expect(
      reportRefStrings([
        { type: "callout", data: { tone: "info", text: "See card:a" } },
        { type: "explanation", data: { text: "Then doc:b" } },
      ]),
    ).toEqual(["See card:a", "Then doc:b"]);
  });

  it("collects milestone labels and details", () => {
    expect(
      reportRefStrings([
        {
          type: "milestone-timeline",
          data: {
            items: [{ label: "Ship card:a", detail: "after doc:b" }],
          },
        },
      ]),
    ).toEqual(["Ship card:a", "after doc:b"]);
  });

  it("collects diagram node and edge labels", () => {
    expect(
      reportRefStrings([
        {
          type: "dependency-diagram",
          data: {
            nodes: [{ id: "n", label: "card:a", status: "pending" }],
            edges: [{ from: "n", to: "m", label: "needs doc:b" }],
          },
        },
      ]),
    ).toEqual(["card:a", "needs doc:b"]);
  });

  it("collects a live initiative's summary, where its only refs are", () => {
    // The authored panel is a query: no prose, no refs. Everything a reader
    // sees in a live panel arrives with the observation, so collecting from
    // the authored panel asked for nothing and every ref written in a live
    // summary rendered dashed and "not found".
    const authored = {
      id: "initiatives",
      type: "live-initiatives",
      data: { limit: 5 },
    };
    expect(reportRefStrings([authored])).toEqual([]);

    const observed = {
      ...authored,
      live: {
        status: "ok",
        data: {
          items: [
            {
              ref: "card:release-b",
              title: "Release B",
              summary: "Blocked behind card:adapter-contract until Friday.",
            },
          ],
        },
      },
    };
    expect(reportRefStrings([observed])).toEqual([
      "card:release-b",
      "Blocked behind card:adapter-contract until Friday.",
    ]);
  });

  it("asks for nothing from a live panel that has not answered yet", () => {
    // A loading or failed observation has no prose to scan, and asking for a
    // half-read panel's refs would resolve a set the reader cannot see.
    for (const live of [
      undefined,
      { status: "loading", data: {} },
      { status: "unavailable", message: "no access" },
      { status: "ok", data: {} },
    ]) {
      expect(
        reportRefStrings([
          { type: "live-initiatives", data: { limit: 5 }, live },
        ]),
      ).toEqual([]);
    }
  });

  it("keeps collecting authored text from a panel that also has live data", () => {
    // Live collection is additive: a report mixes authored panels with live
    // ones, and the batch has to carry both.
    expect(
      reportRefStrings([
        { type: "explanation", data: { text: "See card:a" } },
        {
          type: "live-initiatives",
          data: { limit: 5 },
          live: {
            status: "ok",
            data: { items: [{ ref: "card:b", summary: "After card:c." }] },
          },
        },
      ]),
    ).toEqual(["See card:a", "card:b", "After card:c."]);
  });

  it("ignores panels with no ref-bearing text", () => {
    expect(
      reportRefStrings([
        { type: "chart", data: { option: { series: [] } } },
        { type: "metric-chart", data: { points: [] } },
      ]),
    ).toEqual([]);
    expect(reportRefStrings()).toEqual([]);
  });
});
