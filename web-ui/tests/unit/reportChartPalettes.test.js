import { describe, expect, it } from "vitest";

import {
  buildReportChartOption,
  contrastRatio,
  relativeLuminance,
  REPORT_CHART_PALETTES,
  reportSeriesColors,
} from "../../src/lib/visualReportCharts.js";

/**
 * The surfaces the ramps are designed against. `appearance.bg` can be
 * overridden per deployment, so these are the defaults a ramp must clear.
 */
const DARK_BG = "#0b0d12";
const LIGHT_BG = "#ffffff";

/**
 * WCAG 1.4.11: graphical objects need 3:1 against their background. Series
 * marks are graphical objects, not text, so 3:1 is the right bar for a line or
 * a bar against the panel.
 */
const MARK_CONTRAST = 3;

/**
 * The defect this replaced: two series the same lightness in different hues,
 * 1.01:1 apart. Any pair closer than this in relative luminance is effectively
 * one colour to a greyscale or colour-blind reader.
 */
const MIN_PAIRWISE_LUMINANCE_GAP = 0.02;

const names = Object.keys(REPORT_CHART_PALETTES);

describe("relativeLuminance", () => {
  it("matches the WCAG endpoints", () => {
    expect(relativeLuminance("#000000")).toBeCloseTo(0, 5);
    expect(relativeLuminance("#ffffff")).toBeCloseTo(1, 5);
  });

  it("rejects anything that is not a six-digit hex colour", () => {
    expect(relativeLuminance("#fff")).toBeNaN();
    expect(relativeLuminance("rebeccapurple")).toBeNaN();
    expect(relativeLuminance(undefined)).toBeNaN();
  });
});

describe("contrastRatio", () => {
  it("reports the known black-on-white ratio", () => {
    expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 2);
  });

  it("is order independent", () => {
    expect(contrastRatio("#0b0d12", "#d9f99d")).toBeCloseTo(
      contrastRatio("#d9f99d", "#0b0d12"),
      6,
    );
  });
});

describe("report chart palettes", () => {
  it("keeps the four contract names", () => {
    // contracts/visualreport/chart.go validates these names; renaming one here
    // would silently reject published reports.
    expect(names.sort()).toEqual(
      ["categorical", "forest", "ocean", "sunset"].sort(),
    );
  });

  it("gives every palette a ramp per theme", () => {
    for (const name of names) {
      expect(REPORT_CHART_PALETTES[name].dark).toHaveLength(6);
      expect(REPORT_CHART_PALETTES[name].light).toHaveLength(6);
    }
  });

  for (const name of names) {
    for (const [theme, background] of [
      ["dark", DARK_BG],
      ["light", LIGHT_BG],
    ]) {
      describe(`${name} / ${theme}`, () => {
        const colors = REPORT_CHART_PALETTES[name][theme];

        it("descends in lightness so series differ by more than hue", () => {
          const luminances = colors.map(relativeLuminance);
          for (let index = 1; index < luminances.length; index += 1) {
            expect(luminances[index]).toBeLessThan(luminances[index - 1]);
          }
        });

        it("keeps every pair of series apart in lightness", () => {
          const sorted = colors.map(relativeLuminance).sort((a, b) => a - b);
          for (let index = 1; index < sorted.length; index += 1) {
            expect(sorted[index] - sorted[index - 1]).toBeGreaterThanOrEqual(
              MIN_PAIRWISE_LUMINANCE_GAP,
            );
          }
        });

        it("clears 3:1 against its own background", () => {
          for (const color of colors) {
            expect(contrastRatio(color, background)).toBeGreaterThanOrEqual(
              MARK_CONTRAST,
            );
          }
        });

        it("uses well-formed hex colours", () => {
          for (const color of colors) {
            expect(color).toMatch(/^#[0-9a-f]{6}$/);
          }
        });
      });
    }
  }

  it("keeps the palettes distinguishable from each other", () => {
    for (const theme of ["dark", "light"]) {
      for (const a of names) {
        for (const b of names) {
          if (a >= b) continue;
          const shared = REPORT_CHART_PALETTES[a][theme].filter((color) =>
            REPORT_CHART_PALETTES[b][theme].includes(color),
          );
          expect(shared.length).toBeLessThanOrEqual(2);
        }
      }
    }
  });
});

describe("reportSeriesColors", () => {
  it("picks the light ramp for a light surface and the dark ramp for a dark one", () => {
    expect(reportSeriesColors("ocean", LIGHT_BG)).toEqual(
      REPORT_CHART_PALETTES.ocean.light,
    );
    expect(reportSeriesColors("ocean", DARK_BG)).toEqual(
      REPORT_CHART_PALETTES.ocean.dark,
    );
  });

  it("decides from the surface, not a theme name, so custom tokens still work", () => {
    expect(reportSeriesColors("forest", "#f8fafc")).toEqual(
      REPORT_CHART_PALETTES.forest.light,
    );
    expect(reportSeriesColors("forest", "#1e1b16")).toEqual(
      REPORT_CHART_PALETTES.forest.dark,
    );
  });

  it("falls back to the dark ramp when the surface is unreadable", () => {
    expect(reportSeriesColors("ocean", "not-a-colour")).toEqual(
      REPORT_CHART_PALETTES.ocean.dark,
    );
  });

  it("falls back to ocean for an unknown palette name", () => {
    expect(reportSeriesColors("neon", DARK_BG)).toEqual(
      REPORT_CHART_PALETTES.ocean.dark,
    );
  });

  it("returns a copy so a caller cannot mutate the palette", () => {
    const colors = reportSeriesColors("ocean", DARK_BG);
    colors[0] = "#000000";
    expect(REPORT_CHART_PALETTES.ocean.dark[0]).not.toBe("#000000");
  });
});

describe("buildReportChartOption palette and legend wiring", () => {
  const chart = (palette) => ({
    ...(palette ? { palette } : {}),
    option: {
      xAxis: { type: "category", data: ["Mon", "Tue"] },
      yAxis: { type: "value" },
      series: [
        { type: "line", name: "Opened", data: [2, 4] },
        { type: "line", name: "Closed", data: [1, 3] },
      ],
    },
  });

  it("takes series colours from the theme background", () => {
    const dark = buildReportChartOption(chart(), { bg: DARK_BG });
    const light = buildReportChartOption(chart(), { bg: LIGHT_BG });
    expect(dark.color).toEqual(REPORT_CHART_PALETTES.ocean.dark);
    expect(light.color).toEqual(REPORT_CHART_PALETTES.ocean.light);
  });

  it("honours the authored palette name", () => {
    const option = buildReportChartOption(chart("sunset"), { bg: DARK_BG });
    expect(option.color).toEqual(REPORT_CHART_PALETTES.sunset.dark);
  });

  it("hides the built-in legend so the Svelte one owns it", () => {
    const option = buildReportChartOption(chart(), { bg: DARK_BG });
    expect(option.legend.show).toBe(false);
    expect(option.legend.type).toBeUndefined();
  });

  it("renders tooltips as html so the formatter can return an element", () => {
    const option = buildReportChartOption(chart(), { bg: DARK_BG });
    expect(option.tooltip.renderMode).toBe("html");
    expect(option.tooltip.trigger).toBe("axis");
  });

  it("no longer reserves in-canvas space for a legend", () => {
    const option = buildReportChartOption(chart(), { bg: DARK_BG });
    // Room for the axis labels and nothing else: the legend is Svelte's.
    expect(option.grid.bottom).toBe(30);
    expect(option.grid.left).toBe(44);
  });

  it("reserves axis-name room only for an axis that has a name", () => {
    const named = chart();
    named.option.xAxis = { ...named.option.xAxis, name: "Day" };
    named.option.yAxis = { type: "value", name: "cards" };
    const option = buildReportChartOption(named, { bg: DARK_BG });
    // A rotated axis name needs the wider gutter; a chart with no names —
    // which is every series-bound one — spent it on empty margin, and on a
    // phone that left the plot in a letterbox.
    expect(option.grid.bottom).toBe(52);
    expect(option.grid.left).toBe(52);
  });
});
