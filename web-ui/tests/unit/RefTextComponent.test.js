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
    const link = container.querySelector("a.ref-text__link");
    expect(link.getAttribute("href")).toBe("https://example.test/notes");
    expect(link.getAttribute("rel")).toBe("noreferrer noopener");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("renders a known pull request as a chip, not a bare link", () => {
    const { container } = mount(
      "fixed in https://github.com/Git-on-my-level/agent-nexus/pull/246",
    );
    expect(container.querySelector("[data-anx-ref]")).not.toBeNull();
    expect(container.querySelector("a.ref-text__link")).toBeNull();
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
