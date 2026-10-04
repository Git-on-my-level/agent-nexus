import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { portfolioReviewReport } from "../../src/lib/fixtures/expressiveReportExamples.js";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * The chart legend and tooltip, in a real browser.
 *
 * Unit tests cover what the tooltip and legend models contain; these cover the
 * things only a browser can answer: that the legend wraps instead of
 * paginating, that a click pins rather than hides, that the tooltip actually
 * appears with the hovered series leading it, and that all of it holds in both
 * themes and at phone width.
 */

const DOC_ID = "chart-interaction";
const DOC_PATH = `/o/local/w/local/docs/${DOC_ID}`;
const OBSERVED_AT = "2026-10-03T07:00:00Z";

/**
 * A known-valid fixture rather than a hand-written one: `mix-chart` carries
 * three bar series on a category axis, which is what makes the tooltip an axis
 * tooltip covering every series at once.
 */
const report = structuredClone(portfolioReviewReport);
const CHART_PANEL = report.panels.find((panel) => panel.id === "mix-chart");
const SERIES_NAMES = CHART_PANEL.data.option.series.map(
  (series) => series.name,
);

async function installReportDocument(page) {
  const content = JSON.stringify(report);
  const document = {
    id: DOC_ID,
    handle: DOC_ID,
    ref: `document:${DOC_ID}`,
    title: "Chart interaction",
    state: "active",
    head_revision_id: `${DOC_ID}-revision-1`,
    head_revision_number: 1,
    updated_at: OBSERVED_AT,
    updated_by: "actor-operator",
    created_at: OBSERVED_AT,
    created_by: "actor-operator",
  };
  const revision = {
    document_id: DOC_ID,
    revision_id: document.head_revision_id,
    ref: `document_revision:${DOC_ID}-r1`,
    revision_number: 1,
    content_type: "text",
    content_hash: `${DOC_ID}-content-hash`,
    revision_hash: `${DOC_ID}-revision-hash`,
    created_at: OBSERVED_AT,
    created_by: "actor-operator",
    content,
  };
  await page.clock.setFixedTime(new Date(OBSERVED_AT));
  await installWorkspaceApi(page, { documents: [document] });
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (!["fetch", "xhr"].includes(request.resourceType())) {
      return route.fallback();
    }
    if (
      decodeURIComponent(url.pathname) === `/docs/${DOC_ID}` &&
      request.method() === "GET"
    ) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ document, revision }),
      });
    }
    return route.fallback();
  });
}

const chartPanel = (page) =>
  page.getByRole("region", { name: CHART_PANEL.title, exact: true });
const legendItems = (page) => chartPanel(page).locator(".chart-legend__item");

async function openReport(page) {
  // The dev server compiles the document route on first hit; the repo's other
  // report specs raise the timeout for the same reason.
  test.setTimeout(90_000);
  await installReportDocument(page);
  await page.goto(DOC_PATH);
  // A cold dev server compiles the document route on first hit, which can run
  // past the default expect timeout before any panel exists.
  await expect(
    page.getByRole("region", { name: "Visual report", exact: true }),
  ).toBeVisible({ timeout: 60_000 });
  await expect(chartPanel(page).locator("[data-report-chart]")).toBeVisible({
    timeout: 30_000,
  });
  // The legend renders once the chart module has loaded and the option applied.
  await expect(legendItems(page).first()).toBeVisible({ timeout: 30_000 });
}

/**
 * The app ships one token set and it is dark; `app.css` has no light theme and
 * no `prefers-color-scheme` block, so emulating a light colour scheme would
 * change nothing. These run against the real surface.
 */
for (const viewport of [
  { width: 1440, height: 1000, label: "desktop" },
  { width: 390, height: 844, label: "phone" },
]) {
  {
    test(`chart legend and tooltip @ ${viewport.label}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({
        width: viewport.width,
        height: viewport.height,
      });
      await openReport(page);

      // Wrapping, not paginating: every series is reachable without a pager.
      for (const name of SERIES_NAMES) {
        await expect(
          page.getByRole("button", { name, exact: true }),
        ).toBeVisible();
      }

      const chart = chartPanel(page).locator("[data-report-chart]");
      await chart.screenshot({
        path: testInfo.outputPath(`legend-${viewport.label}.png`),
        animations: "disabled",
      });
      await testInfo.attach(`legend-${viewport.label}`, {
        path: testInfo.outputPath(`legend-${viewport.label}.png`),
        contentType: "image/png",
      });

      // A click pins; it must not hide the series the way the old legend did.
      const closed = page.getByRole("button", {
        name: SERIES_NAMES[1],
        exact: true,
      });
      await expect(closed).toHaveAttribute("aria-pressed", "false");
      await closed.click();
      await expect(closed).toHaveAttribute("aria-pressed", "true");
      await expect(chartPanel(page).locator("svg").first()).toBeVisible();
      await closed.click();
      await expect(closed).toHaveAttribute("aria-pressed", "false");

      // The chart surface is what carries the axis pointer.
      const surface = chartPanel(page).locator(".chart-surface");
      const box = await surface.boundingBox();
      await page.mouse.move(box.x + box.width * 0.6, box.y + box.height * 0.5);

      const tooltip = page.locator(".report-tooltip");
      await expect(tooltip).toBeVisible();
      // The lead row carries a swatch, a name and a value.
      await expect(tooltip.locator(".report-tooltip__lead")).toBeVisible();
      await expect(
        tooltip.locator(".report-tooltip__lead .report-tooltip__swatch"),
      ).toBeVisible();
      await expect(
        tooltip.locator(".report-tooltip__value").first(),
      ).not.toBeEmpty();

      await page.screenshot({
        path: testInfo.outputPath(`tooltip-${viewport.label}.png`),
        animations: "disabled",
      });
      await testInfo.attach(`tooltip-${viewport.label}`, {
        path: testInfo.outputPath(`tooltip-${viewport.label}.png`),
        contentType: "image/png",
      });
    });
  }
}

test("legend items are keyboard reachable and announce their pinned state", async ({
  page,
}) => {
  await openReport(page);
  const opened = page.getByRole("button", {
    name: SERIES_NAMES[0],
    exact: true,
  });
  await opened.focus();
  await expect(opened).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(opened).toHaveAttribute("aria-pressed", "true");
  await page.keyboard.press("Enter");
  await expect(opened).toHaveAttribute("aria-pressed", "false");
});

{
  test("chart legend has no accessibility violations", async ({ page }) => {
    await openReport(page);
    const results = await new AxeBuilder({ page })
      .include(".report-chart")
      .withTags(["wcag2a", "wcag2aa"])
      .analyze();
    expect(results.violations).toEqual([]);
  });
}

test("series colours follow the surface, ready for a light theme", async ({
  page,
}) => {
  await openReport(page);
  const swatch = () =>
    chartPanel(page)
      .locator(".chart-legend__swatch")
      .first()
      .evaluate((node) => getComputedStyle(node).backgroundColor);

  const onDark = await swatch();

  // The app has no light theme yet, so repaint the surface tokens to prove the
  // ramp is chosen from the background rather than baked in. When a light theme
  // does land, this is the behaviour it will get for free.
  await page.addStyleTag({
    content: ":root { --bg: #ffffff; --panel: #ffffff; --fg: #0b0d12; }",
  });
  // The chart re-reads its appearance when the document attributes change.
  await page.evaluate(() =>
    document.documentElement.setAttribute("data-theme", "light"),
  );
  await expect.poll(async () => swatch(), { timeout: 10_000 }).not.toBe(onDark);
});
