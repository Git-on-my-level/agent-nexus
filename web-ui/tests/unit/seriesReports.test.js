// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";
import { withLiveObservation } from "../../src/lib/liveReports.js";
import { getPanelFreshness } from "../../src/lib/visualReports.js";
import SeriesReportPanel from "../../src/lib/components/reports/SeriesReportPanel.svelte";
afterEach(cleanup);
const definition = () => ({
  type: "metric",
  title: "Builds",
  data: {},
  freshness: "unavailable",
  observed_at: null,
  source: { series: "builds" },
  fallback: {
    as_of: "2026-09-01T00:00:00Z",
    data: { value: 12, unit: "builds" },
  },
});
const observation = () => ({
  status: "ok",
  data: { value: 20, unit: "builds" },
  observed_at: "2026-10-05T00:00:00Z",
  provenance: {
    adapter: "github",
    host: "fleet",
    last_push: "2026-10-05T00:00:01Z",
    expected_interval_seconds: 60,
  },
});
describe("series observations", () => {
  it("shows live values and their source", () => {
    const panel = withLiveObservation(definition(), observation());
    render(SeriesReportPanel, { panel, freshness: "current" });
    expect(screen.getByText(/^20$/)).toBeTruthy();
    // Adapter, host and resolution are one tooltip on the series name now,
    // not a four-line fold under every panel.
    expect(
      screen
        .getByRole("button", { name: /Where this number comes from/ })
        .getAttribute("data-tooltip"),
    ).toMatch(/Pushed by github on fleet/);
    expect(screen.queryByText(/As of/)).toBeNull();
  });
  it.each(["stale", "unavailable", "loading"])(
    "marks fallback as of for %s without upgrading freshness",
    (status) => {
      const panel = withLiveObservation(definition(), {
        ...observation(),
        status,
      });
      expect(panel.data.value).toBe(12);
      expect(getPanelFreshness(panel)).toBe("stale");
      render(SeriesReportPanel, { panel, freshness: "stale" });
      expect(screen.getByText("As of 2026-09-01T00:00:00Z")).toBeTruthy();
    },
  );
  it("shows stale since when no fallback exists", () => {
    const original = definition();
    delete original.fallback;
    const panel = withLiveObservation(original, {
      ...observation(),
      status: "stale",
      stale_since: "2026-10-05T00:02:00Z",
    });
    render(SeriesReportPanel, { panel, freshness: "stale" });
    expect(screen.getByText(/Stale since 2026-10-05T00:02:00Z/)).toBeTruthy();
    expect(screen.queryByText(/20 builds/)).toBeNull();
  });
  it("ages a successful observation by the declared interval", () => {
    const panel = withLiveObservation(definition(), observation());
    expect(getPanelFreshness(panel, Date.parse("2026-10-05T00:02:00Z"))).toBe(
      "current",
    );
    expect(getPanelFreshness(panel, Date.parse("2026-10-05T00:02:01Z"))).toBe(
      "stale",
    );
  });
});
