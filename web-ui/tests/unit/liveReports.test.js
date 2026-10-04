import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  validateLiveQuery,
  withLiveObservation,
  formatLiveAge,
} from "../../src/lib/liveReports.js";
import {
  parseVisualReport,
  getPanelFreshness,
} from "../../src/lib/visualReports.js";
import { visualReportExample } from "../../src/lib/fixtures/visualReportExample.js";

const cases = JSON.parse(
  readFileSync(
    new URL(
      "../../../contracts/fixtures/visual-reports/queries.json",
      import.meta.url,
    ),
    "utf8",
  ),
);

describe("canonical live query fixtures", () => {
  for (const item of cases) {
    it(item.name, () => {
      expect(validateLiveQuery(item.type, item.data).length === 0).toBe(
        item.valid,
      );
      const report = structuredClone(visualReportExample);
      report.panels[0].type = item.type;
      report.panels[0].data = item.data;
      expect(Boolean(parseVisualReport(JSON.stringify(report)).report)).toBe(
        item.valid,
      );
    });
  }
});

it("keeps static snapshots and uses read time for live freshness", () => {
  const staticPanel = visualReportExample.panels[0];
  expect(withLiveObservation(staticPanel, null)).toBe(staticPanel);
  const panel = { ...staticPanel, type: "live-initiatives" };
  const now = Date.parse("2026-10-04T10:00:00Z");
  expect(getPanelFreshness(panel, now)).toBe("unavailable");
  const observed = withLiveObservation(panel, {
    status: "ok",
    observed_at: "2026-10-04T10:00:00.123456789Z",
    data: { items: [] },
  });
  expect(getPanelFreshness(observed, now)).toBe("current");
  expect(
    getPanelFreshness(
      withLiveObservation(panel, { status: "unavailable" }),
      now,
    ),
  ).toBe("unavailable");
  expect(panel.live).toBeUndefined();
});

it("formats ask age without implying freshness or a deadline", () => {
  expect(formatLiveAge(0)).toBe("Just now");
  expect(formatLiveAge(3600)).toBe("1h old");
  expect(formatLiveAge(86400)).toBe("1d old");
});
