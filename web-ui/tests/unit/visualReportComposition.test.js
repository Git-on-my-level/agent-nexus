import { describe, expect, it } from "vitest";
import {
  parseVisualReport,
  VISUAL_REPORT_TYPES,
} from "../../src/lib/visualReports.js";
import {
  swarmObservatoryReport,
  portfolioReviewReport,
} from "../../src/lib/fixtures/expressiveReportExamples.js";
import { liveDashboardExample } from "../../src/lib/fixtures/liveDashboardExample.js";
import { seriesDashboardExample } from "../../src/lib/fixtures/seriesDashboardExample.js";
import { visualReportExample } from "../../src/lib/fixtures/visualReportExample.js";
import { validateReportLayout } from "../../src/lib/visualReportLayout.js";
const parse = (report) => parseVisualReport(JSON.stringify(report));
describe("expressive composition", () => {
  it("dogfoods every supported panel type", () => {
    expect(
      new Set(
        [
          visualReportExample,
          swarmObservatoryReport,
          portfolioReviewReport,
          liveDashboardExample,
          seriesDashboardExample,
        ].flatMap((report) => report.panels.map((panel) => panel.type)),
      ),
    ).toEqual(new Set(VISUAL_REPORT_TYPES));
  });
  for (const report of [
    swarmObservatoryReport,
    portfolioReviewReport,
    liveDashboardExample,
    seriesDashboardExample,
  ]) {
    it(`accepts ${report.title}`, () => {
      expect(parse(report).errors).toEqual([]);
    });
  }
  it("rejects duplicate and missing panel references", () => {
    expect(
      validateReportLayout(
        {
          type: "stack",
          children: [
            { type: "panel", panel_id: "x" },
            { type: "panel", panel_id: "x" },
            { type: "panel", panel_id: "missing" },
          ],
        },
        new Set(["x"]),
      ),
    ).toHaveLength(2);
  });
  it("bounds depth and rejects executable or styling fields", () => {
    let node = { type: "panel", panel_id: "x" };
    for (let i = 0; i < 8; i++) node = { type: "stack", children: [node] };
    expect(validateReportLayout(node, new Set(["x"])).join()).toContain(
      "levels",
    );
    expect(
      validateReportLayout(
        { type: "panel", panel_id: "x", style: "position:fixed" },
        new Set(["x"]),
      ).join(),
    ).toContain("unsupported");
  });
  it("rejects duplicate tab ids, invalid spans and oversized tabs", () => {
    const tab = {
      type: "tabs",
      id: "t",
      items: [
        { id: "a", label: "A", children: [{ type: "panel", panel_id: "x" }] },
      ],
    };
    expect(
      validateReportLayout(
        { type: "grid", columns: 2, span: 5, children: [tab, tab] },
        new Set(["x"]),
      ).length,
    ).toBeGreaterThan(0);
  });
  it("keeps null distinct from zero and rejects unlabelled sparklines", () => {
    const report = structuredClone(swarmObservatoryReport);
    delete report.panels[0].data.items[0].trend_label;
    expect(parse(report).errors.join()).toContain("trend_label");
    report.panels[0].data.items[0].trend_label = "Daily series";
    report.panels[0].data.items[0].trend = [0, null, 3];
    expect(parse(report).errors.join()).toContain("finite");
  });
  it("accepts only application-owned appearance tokens", () => {
    const report = structuredClone(swarmObservatoryReport);
    report.panels[0].appearance = "soft";
    report.panels[0].density = "compact";
    expect(parse(report).errors).toEqual([]);
    report.panels[0].appearance = "url(https://example.org)";
    expect(parse(report).report).toBeNull();
  });
  it("rejects unsupported metric and comparison fields", () => {
    const report = structuredClone(swarmObservatoryReport);
    report.panels[0].data.items[0].onclick = "alert(1)";
    expect(parse(report).report).toBeNull();
    const comparison = structuredClone(portfolioReviewReport);
    comparison.panels.find(
      (p) => p.type === "comparison",
    ).data.items[0].verdict = "approved";
    expect(parse(comparison).report).toBeNull();
  });
});
