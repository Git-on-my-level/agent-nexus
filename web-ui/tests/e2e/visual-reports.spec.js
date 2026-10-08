import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname } from "node:path";

import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import {
  swarmObservatoryReport,
  portfolioReviewReport,
} from "../../src/lib/fixtures/expressiveReportExamples.js";
import { liveDashboardExample } from "../../src/lib/fixtures/liveDashboardExample.js";
import { visualReportExample } from "../../src/lib/fixtures/visualReportExample.js";
import { expectCleanLayout } from "../helpers/layoutAudit.js";
import {
  expectNoClippedContent,
  installWorkspaceApi,
} from "../helpers/workspaceApiMock.js";

const DOC_ID = "visual-report";
const DOC_PATH = `/o/local/w/local/docs/${DOC_ID}`;
const OBSERVED_AT = "2026-10-03T07:00:00Z";
const REPORT_REQUEST = /^\/(docs|events|artifacts|reports)(\/|$)/;
const cloneExample = () => structuredClone(visualReportExample);

/**
 * Exercise the existing document reader, not a special report/demo endpoint.
 * Public links are evidence only: fail if rendering attempts to fetch one.
 */
async function installReportDocument(page, report = cloneExample()) {
  const content = typeof report === "string" ? report : JSON.stringify(report);
  const document = {
    id: DOC_ID,
    handle: DOC_ID,
    ref: `document:${DOC_ID}`,
    title: "Visual reporting example",
    state: "active",
    head_revision_id: "visual-report-revision-1",
    head_revision_number: 1,
    updated_at: OBSERVED_AT,
    updated_by: "actor-operator",
    created_at: OBSERVED_AT,
    created_by: "actor-operator",
  };
  const revision = {
    document_id: DOC_ID,
    revision_id: document.head_revision_id,
    ref: "document_revision:dashboard-r1",
    revision_number: 1,
    content_type: "text",
    content_hash: "visual-report-content-hash",
    revision_hash: "visual-report-revision-hash",
    created_at: OBSERVED_AT,
    created_by: "actor-operator",
    content,
  };
  const state = {
    content,
    documentReads: 0,
    requests: [],
    externalRequests: [],
  };
  await page.clock.setFixedTime(new Date(OBSERVED_AT));
  await installWorkspaceApi(page, { documents: [document] });
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (
      /^https?:$/.test(url.protocol) &&
      !["127.0.0.1", "localhost", "[::1]"].includes(url.hostname)
    ) {
      state.externalRequests.push(request.url());
      return route.abort("blockedbyclient");
    }
    if (!["fetch", "xhr"].includes(request.resourceType())) {
      return route.fallback();
    }
    const path = decodeURIComponent(url.pathname);
    if (REPORT_REQUEST.test(path)) {
      state.requests.push({ path, method: request.method() });
    }
    if (path === `/docs/${DOC_ID}` && request.method() === "GET") {
      state.documentReads += 1;
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ document, revision }),
      });
    }
    return route.fallback();
  });
  return state;
}

function reportRegion(page) {
  return page.getByRole("region", { name: "Visual report", exact: true });
}

function panelRegion(report, panel) {
  return report.getByRole("region", { name: panel.title, exact: true });
}

async function expectPanels(report, visible, hidden = []) {
  for (const panel of visible) {
    await expect(panelRegion(report, panel)).toBeVisible();
  }
  for (const panel of hidden) {
    await expect(panelRegion(report, panel)).toHaveCount(0);
  }
}

function expectReadOnly(state) {
  expect(state.documentReads).toBeGreaterThan(0);
  expect(state.requests.filter(({ method }) => method !== "GET")).toEqual([]);
  expect(
    state.requests.filter(({ path }) => path.startsWith("/reports")),
  ).toEqual([]);
  expect(state.externalRequests).toEqual([]);
}

async function expectQuery(page, values) {
  await expect(page).toHaveURL((url) =>
    Object.entries(values).every(
      ([key, value]) => url.searchParams.get(key) === value,
    ),
  );
}

async function saveScreenshot(locator, testInfo, name) {
  const path = testInfo.outputPath(`${name}.png`);
  await locator.screenshot({ path, animations: "disabled" });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 900, height: 1000 },
  { width: 390, height: 844 },
]) {
  test(`visual report states @ ${viewport.width}`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(90_000);
    await page.setViewportSize(viewport);
    const example = cloneExample();
    const sourcePath = testInfo.outputPath("anx-public-rollout-report.json");
    await mkdir(dirname(sourcePath), { recursive: true });
    await writeFile(sourcePath, JSON.stringify(example, null, 2), "utf8");
    // Local fixture roundtrip through the existing document read response.
    // This does not create a document in a deployed core workspace.
    const savedContent = await readFile(sourcePath, "utf8");
    expect(JSON.parse(savedContent)).toEqual(example);
    const state = await installReportDocument(page, savedContent);
    await page.goto(DOC_PATH);
    const report = reportRegion(page);
    await expect(
      report.getByRole("heading", { name: example.title }),
    ).toBeVisible();
    await expectPanels(report, example.panels);
    await expectCleanLayout(page, "visual report overview", {
      scrollPositions: ["top", "bottom"],
    });
    // The ANX shell owns the scroll container, not necessarily window.
    // Capture visible viewports rather than a tall element clipped by that shell.
    await report.evaluate((element) => {
      for (let node = element; node; node = node.parentElement) {
        node.scrollTop = 0;
        node.scrollLeft = 0;
      }
      window.scrollTo(0, 0);
    });
    await saveScreenshot(page, testInfo, `report-page-${viewport.width}`);
    await testInfo.attach("visual-report-source", {
      path: sourcePath,
      contentType: "application/json",
    });

    for (const type of [
      "milestone-timeline",
      "dependency-diagram",
      "metric-chart",
      "artifact-preview",
    ]) {
      const panel = example.panels.find((item) => item.type === type);
      await panelRegion(report, panel).evaluate((element) =>
        element.scrollIntoView({ block: "start", behavior: "instant" }),
      );
      const path = testInfo.outputPath(`report-${type}-${viewport.width}.png`);
      await page.screenshot({ path, animations: "disabled" });
      await testInfo.attach(`report-${type}-${viewport.width}`, {
        path,
        contentType: "image/png",
      });
    }

    const evidencePanel = example.panels.find(
      (panel) => panel.type === "evidence-table" && panel.source_ids.length,
    );
    expect(evidencePanel).toBeTruthy();
    const evidenceRegion = panelRegion(report, evidencePanel);
    const inspect = evidenceRegion.getByRole("button", {
      name: "Inspect evidence",
    });
    await inspect.click();
    await expectQuery(page, { reportEvidence: evidencePanel.id });
    await expect(inspect).toHaveAttribute("aria-expanded", "true");
    for (const sourceId of evidencePanel.source_ids) {
      const source = example.sources.find((item) => item.id === sourceId);
      await expect(
        evidenceRegion.getByRole("link", { name: source.label }),
      ).toHaveAttribute("href", source.url);
    }
    await expectCleanLayout(page, "visual report evidence expanded");
    if (viewport.width >= 900) {
      await saveScreenshot(
        evidenceRegion,
        testInfo,
        `report-evidence-${viewport.width}`,
      );
    } else {
      await evidenceRegion
        .getByRole("heading", { name: "Source evidence" })
        .evaluate((element) =>
          element.scrollIntoView({ block: "start", behavior: "instant" }),
        );
      const path = testInfo.outputPath(`report-evidence-${viewport.width}.png`);
      await page.screenshot({ path, animations: "disabled" });
      await testInfo.attach(`report-evidence-${viewport.width}`, {
        path,
        contentType: "image/png",
      });
    }

    await page.goBack();
    await expectQuery(page, { reportEvidence: null });
    await expect(inspect).toHaveAttribute("aria-expanded", "false");
    await page.goForward();
    await expectQuery(page, { reportEvidence: evidencePanel.id });
    await expect(inspect).toHaveAttribute("aria-expanded", "true");
    await inspect.click();
    await expectQuery(page, { reportEvidence: null });

    const project = example.projects[0];
    const projectPanels = example.panels.filter(
      (panel) => panel.project_id === project.id,
    );
    const otherPanels = example.panels.filter(
      (panel) => panel.project_id !== project.id,
    );
    await report.getByRole("button", { name: project.title }).click();
    await expectQuery(page, { reportProject: project.id });
    await expectPanels(report, projectPanels, otherPanels);
    const freshness = report.getByLabel("Filter by freshness", { exact: true });
    await freshness.selectOption("unavailable");
    await expectQuery(page, {
      reportProject: project.id,
      reportFreshness: "unavailable",
    });
    await expect(freshness).toHaveValue("unavailable");
    await expectPanels(
      report,
      projectPanels.filter((panel) => panel.freshness === "unavailable"),
      projectPanels.filter((panel) => panel.freshness !== "unavailable"),
    );
    await page.goBack();
    await expectQuery(page, {
      reportProject: project.id,
      reportFreshness: null,
    });
    await expect(freshness).toHaveValue("all");
    await expectPanels(report, projectPanels, otherPanels);
    await page.goForward();
    await expect(freshness).toHaveValue("unavailable");
    await expectQuery(page, { reportFreshness: "unavailable" });

    await freshness.selectOption("all");
    await report
      .getByRole("button", { name: "All projects", exact: true })
      .click();
    await expectQuery(page, { reportProject: null, reportFreshness: null });
    await expectPanels(report, example.panels);
    await page.goBack();
    await expectQuery(page, {
      reportProject: project.id,
      reportFreshness: null,
    });
    await expectPanels(report, projectPanels, otherPanels);
    await page.goForward();
    await expectQuery(page, { reportProject: null, reportFreshness: null });
    await expectPanels(report, example.panels);
    expectReadOnly(state);
  });
}

test("stale, unknown and unavailable evidence stay explicit when filtering", async ({
  page,
}) => {
  const example = cloneExample();
  // A deliberate stale state, kept out of the dated public example itself.
  example.panels[0].freshness = "current";
  example.panels[0].observed_at = "2026-09-01T00:00:00Z";
  const stalePanel = example.panels[0];
  const state = await installReportDocument(page, example);
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  const freshness = report.getByLabel("Filter by freshness", { exact: true });
  await freshness.selectOption("stale");
  await expectQuery(page, { reportFreshness: "stale" });
  await expect(panelRegion(report, stalePanel)).toBeVisible();
  await expect(panelRegion(report, stalePanel)).toContainText(/stale/i);
  const effectiveFreshness = (panel) =>
    panel.id === stalePanel.id ? "stale" : panel.freshness;
  for (const status of ["current", "unknown", "unavailable"]) {
    const panels = example.panels.filter(
      (panel) => effectiveFreshness(panel) === status,
    );
    expect(
      panels.length,
      `Example includes ${status} evidence`,
    ).toBeGreaterThan(0);
    await freshness.selectOption(status);
    await expectQuery(page, { reportFreshness: status });
    await expectPanels(
      report,
      panels,
      example.panels.filter((panel) => effectiveFreshness(panel) !== status),
    );
    for (const panel of panels) {
      await expect(panelRegion(report, panel)).toContainText(
        new RegExp(status, "i"),
      );
    }
  }
  expectReadOnly(state);
});

test("report project and evidence deep links survive reload", async ({
  page,
}) => {
  const example = cloneExample();
  const panel = example.panels.find(
    (item) => item.type === "evidence-table" && item.source_ids.length,
  );
  const query = new URLSearchParams({
    reportProject: panel.project_id,
    reportEvidence: panel.id,
    reportFreshness: "current",
  });
  const state = await installReportDocument(page, example);
  await page.goto(`${DOC_PATH}?${query}`);
  const report = reportRegion(page);
  const inspect = panelRegion(report, panel).getByRole("button", {
    name: "Inspect evidence",
  });
  await expect(inspect).toHaveAttribute("aria-expanded", "true");
  await page.reload();
  await expect(inspect).toHaveAttribute("aria-expanded", "true");
  await expectQuery(page, {
    reportProject: panel.project_id,
    reportEvidence: panel.id,
    reportFreshness: "current",
  });
  expectReadOnly(state);
});

for (const invalid of [
  {
    name: "malformed JSON",
    content: () =>
      '{"kind":"anx.visual-report","schema_version":1,"title":"Broken report",',
  },
  {
    name: "unsupported report version",
    content: () => ({ ...cloneExample(), schema_version: 99 }),
  },
  {
    name: "unsupported report component",
    content: () => {
      const report = cloneExample();
      report.panels[0].type = "unsupported-widget";
      return report;
    },
  },
]) {
  test(`${invalid.name} shows a safe fallback and inspectable source`, async ({
    page,
  }) => {
    const state = await installReportDocument(page, invalid.content());
    await page.goto(DOC_PATH);
    await expect(
      page.getByText("Report cannot be rendered", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(/TypeError:|SyntaxError:|at VisualReport/),
    ).toHaveCount(0);
    const source = page.locator("details").filter({
      has: page.locator("summary", { hasText: "View report source" }),
    });
    await source.locator("summary").click();
    await expect(source.locator("pre")).toHaveText(state.content);
    expectReadOnly(state);
  });
}

test("evidence links remain inert and are never loaded as remote content", async ({
  page,
}) => {
  const example = cloneExample();
  const source = example.sources[0];
  source.url = "https://report-source.example.invalid/never-fetch-this";
  const explanation = example.panels.find(
    (item) => item.type === "explanation",
  );
  explanation.data.text = `<img src="${source.url}" onerror="window.reportContentExecuted = true">`;
  const artifact = example.panels.find(
    (item) => item.type === "artifact-preview",
  );
  artifact.data.excerpt =
    "<script>window.reportContentExecuted = true</script>";
  const panel = example.panels.find((item) =>
    item.source_ids.includes(source.id),
  );
  const state = await installReportDocument(page, example);
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  const region = panelRegion(report, panel);
  await region.getByRole("button", { name: "Inspect evidence" }).click();
  await expect(
    region.getByRole("link", { name: source.label }),
  ).toHaveAttribute("href", source.url);
  // Report prose goes through the shared markdown renderer, which sanitizes:
  // a remote image is dropped outright rather than rendered or echoed back as
  // markup, and its `onerror` never reaches the DOM.
  const explanationBody = panelRegion(report, explanation).locator(
    ".report-explanation",
  );
  await expect(explanationBody).toBeAttached();
  const explanationHtml = await explanationBody.innerHTML();
  expect(explanationHtml).not.toContain("onerror");
  expect(explanationHtml).not.toContain(source.url);
  // An artifact excerpt is raw source by design, shown literally, never run.
  await expect(panelRegion(report, artifact)).toContainText(
    artifact.data.excerpt,
  );
  await expect(report.locator("img, iframe, script")).toHaveCount(0);
  expect(
    await page.evaluate(() => window.reportContentExecuted),
  ).toBeUndefined();
  // Includes img/iframe/script requests, not only fetch/XHR calls.
  expectReadOnly(state);
});

test("ordinary Markdown documents keep their normal document rendering", async ({
  page,
}) => {
  const state = await installReportDocument(
    page,
    "# Ordinary document\n\nExisting Markdown content remains readable.",
  );
  await page.goto(DOC_PATH);
  await expect(
    page.getByRole("heading", { name: "Ordinary document", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Existing Markdown content remains readable."),
  ).toBeVisible();
  await expect(reportRegion(page)).toHaveCount(0);
  await expect(
    page.getByText("Report cannot be rendered", { exact: true }),
  ).toHaveCount(0);
  expectReadOnly(state);
});

test("long accepted report strings stay readable on a 390px screen", async ({
  page,
}) => {
  test.setTimeout(60_000);
  await page.setViewportSize({ width: 390, height: 844 });
  const example = cloneExample();
  example.title = `Report${"R".repeat(194)}`;
  example.summary = "S".repeat(12_000);
  example.projects[0].title = `Project${"P".repeat(193)}`;
  example.projects[0].summary = "D".repeat(400);
  example.projects[0].outcome = "O".repeat(200);
  example.sources[0].label = `Source${"L".repeat(194)}`;
  const panel = example.panels[0];
  panel.title = `Panel${"T".repeat(195)}`;
  panel.data.text = "C".repeat(2_000);
  const state = await installReportDocument(page, example);
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  await expect(
    report.getByRole("heading", { name: example.title, exact: true }),
  ).toBeVisible();
  await expect(
    report.getByText(example.summary, { exact: true }),
  ).toBeVisible();
  await expectCleanLayout(page, "long report strings on mobile", {
    scrollPositions: ["top", "bottom"],
  });
  const project = report.getByRole("button", {
    name: example.projects[0].title,
  });
  await project.scrollIntoViewIfNeeded();
  await expectCleanLayout(page, "long project strings on mobile");
  await expectNoClippedContent(page, "long project strings on mobile");
  const region = panelRegion(report, panel);
  await region.getByRole("button", { name: "Inspect evidence" }).click();
  const source = region.getByRole("link", { name: example.sources[0].label });
  await expect(source).toBeVisible();
  await source.scrollIntoViewIfNeeded();
  await expectCleanLayout(page, "long source label on mobile");
  await expectNoClippedContent(page, "long source label on mobile");
  expectReadOnly(state);
});

test("report controls support keyboard interaction and scoped accessibility checks", async ({
  page,
}) => {
  test.setTimeout(60_000);
  const example = cloneExample();
  const state = await installReportDocument(page, example);
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  const panel = example.panels.find((item) => item.type === "evidence-table");
  const project = example.projects.find((item) => item.id === panel.project_id);
  const projectButton = report.getByRole("button", { name: project.title });
  await projectButton.focus();
  await expect(projectButton).toBeFocused();
  await page.keyboard.press("Enter");
  await expectQuery(page, { reportProject: project.id });
  await expect(projectButton).toHaveAttribute("aria-pressed", "true");
  await expect(projectButton).toBeFocused();
  await page.keyboard.press("Space");
  await expectQuery(page, { reportProject: null });
  await expect(projectButton).toHaveAttribute("aria-pressed", "false");
  await expect(projectButton).toBeFocused();

  const region = panelRegion(report, panel);
  const inspect = region.getByRole("button", { name: "Inspect evidence" });
  await inspect.focus();
  await expect(inspect).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(inspect).toHaveAttribute("aria-expanded", "true");
  await expect(inspect).toBeFocused();
  await expectQuery(page, { reportEvidence: panel.id });
  await expect(
    region.getByRole("heading", { name: "Source evidence" }),
  ).toBeVisible();
  const accessibility = await new AxeBuilder({ page })
    .include('[aria-label="Visual report"]')
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(accessibility.violations).toEqual([]);

  await page.keyboard.press("Space");
  await expect(inspect).toHaveAttribute("aria-expanded", "false");
  await expect(inspect).toBeFocused();
  await expectQuery(page, { reportEvidence: null });
  const source = page.locator("details").filter({
    has: page.locator("summary", { hasText: "View report source" }),
  });
  const disclosure = source.locator("summary");
  await disclosure.focus();
  await expect(disclosure).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(source).toHaveAttribute("open", "");
  await expect(source.locator("pre")).toHaveText(state.content);
  await page.keyboard.press("Space");
  await expect(source).not.toHaveAttribute("open", "");
  await expect(disclosure).toBeFocused();
  expectReadOnly(state);
});

for (const [scenario, example] of [
  ["swarm", swarmObservatoryReport],
  ["portfolio", portfolioReviewReport],
]) {
  for (const viewport of [
    { width: 1440, height: 1100 },
    { width: 390, height: 844 },
  ]) {
    test(`expressive ${scenario} report @ ${viewport.width}`, async ({
      page,
    }, testInfo) => {
      test.setTimeout(120_000);
      await page.setViewportSize(viewport);
      const state = await installReportDocument(page, structuredClone(example));
      await page.goto(DOC_PATH);
      const report = reportRegion(page);
      await expect(
        report.getByRole("heading", { name: example.title, exact: true }),
      ).toBeVisible();
      await expect(report).toContainText("Illustrative scenario");
      const firstChart = report
        .locator("[data-report-panel]")
        .filter({ has: page.locator("[data-report-chart]") })
        .first();
      await expect(firstChart.locator("svg").first()).toBeVisible();
      await saveScreenshot(
        page,
        testInfo,
        `${scenario}-overview-${viewport.width}`,
      );
      await firstChart.scrollIntoViewIfNeeded();
      await saveScreenshot(
        page,
        testInfo,
        `${scenario}-composition-${viewport.width}`,
      );
      const chartDataDisclosure = firstChart.locator("summary", {
        hasText: "View chart data",
      });
      await chartDataDisclosure.click();
      await expect(firstChart.getByRole("table")).toBeVisible();
      await expectCleanLayout(
        page,
        `${scenario} expanded chart data ${viewport.width}`,
      );
      await expectNoClippedContent(
        page,
        `${scenario} expanded chart data ${viewport.width}`,
      );
      const expandedChartAccessibility = await new AxeBuilder({ page })
        .include('[aria-label="Visual report"]')
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      expect(expandedChartAccessibility.violations).toEqual([]);
      await chartDataDisclosure.click();
      await expect(firstChart.getByRole("table")).toBeHidden();
      await expectCleanLayout(
        page,
        `${scenario} composition ${viewport.width}`,
        { scrollPositions: ["top", "bottom"] },
      );
      for (const id of scenario === "swarm"
        ? ["capacity-map", "throughput"]
        : ["quality-frontier", "mix-chart", "dependency-orbit"]) {
        const chartPanel = report.locator(`[data-report-panel="${id}"]`);
        await chartPanel.scrollIntoViewIfNeeded();
        await expect(chartPanel.locator("svg").first()).toBeVisible();
        await saveScreenshot(
          page,
          testInfo,
          `${scenario}-${id}-${viewport.width}`,
        );
      }
      if (scenario === "swarm") {
        const decision = report.getByRole("tab", {
          name: "Decision brief",
          exact: true,
        });
        await decision.focus();
        await page.keyboard.press("Enter");
        await expectQuery(page, { "reportTab.swarm-detail": "decision" });
        await expect(
          report.locator('[data-report-panel="rationale"]'),
        ).toBeVisible();
        await expect(
          report.locator('[data-report-panel="capacity-map"]'),
        ).toHaveCount(0);
        await page.reload();
        await expect(
          report.getByRole("tab", { name: "Decision brief", exact: true }),
        ).toHaveAttribute("aria-selected", "true");
        await saveScreenshot(
          page,
          testInfo,
          `swarm-decision-${viewport.width}`,
        );
        await report
          .getByRole("tab", { name: "Capacity & flow", exact: true })
          .click();
        await page.goBack();
        await expect(
          report.getByRole("tab", { name: "Decision brief", exact: true }),
        ).toHaveAttribute("aria-selected", "true");
      } else {
        await report
          .getByText("Methodology & evidence boundaries", { exact: true })
          .click();
        await expect(
          report.locator('[data-report-panel="methodology"]'),
        ).toBeVisible();
        await report
          .getByText("Methodology & evidence boundaries", { exact: true })
          .click();
        await expect(
          report.locator('[data-report-panel="methodology"]'),
        ).toHaveCount(0);
      }
      const accessibility = await new AxeBuilder({ page })
        .include('[aria-label="Visual report"]')
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();
      expect(accessibility.violations).toEqual([]);
      expectReadOnly(state);
      const jsonPath = testInfo.outputPath(`${scenario}-report.json`);
      await writeFile(jsonPath, JSON.stringify(example, null, 2), "utf8");
      await testInfo.attach(`${scenario}-source`, {
        path: jsonPath,
        contentType: "application/json",
      });
    });
  }
}

test("composed report preserves keyboard, nested evidence, and filtered tab state", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const example = cloneExample();
  const release = example.panels.find((item) => item.id === "rollout-summary");
  const illustrative = example.panels.find(
    (item) => item.id === "example-metrics",
  );
  example.layout = {
    type: "tabs",
    id: "project-detail",
    items: [
      {
        id: "release",
        label: "Release evidence",
        children: [{ type: "panel", panel_id: release.id }],
      },
      {
        id: "example",
        label: "Example evidence",
        children: [
          {
            type: "disclosure",
            title: "Illustrative detail",
            children: [{ type: "panel", panel_id: illustrative.id }],
          },
        ],
      },
    ],
  };
  const state = await installReportDocument(page, example);
  await page.goto(
    `${DOC_PATH}?reportEvidence=${illustrative.id}&preserved=yes`,
  );
  const report = reportRegion(page);
  const releaseTab = report.getByRole("tab", {
    name: "Release evidence",
    exact: true,
  });
  const exampleTab = report.getByRole("tab", {
    name: "Example evidence",
    exact: true,
  });
  const disclosure = report.locator("details").filter({
    has: page.locator("summary", { hasText: "Illustrative detail" }),
  });
  const examplePanel = panelRegion(report, illustrative);
  await expect(exampleTab).toHaveAttribute("aria-selected", "true");
  await expect(disclosure).toHaveAttribute("open", "");
  await expect(
    examplePanel.getByRole("button", { name: "Inspect evidence" }),
  ).toHaveAttribute("aria-expanded", "true");

  await exampleTab.focus();
  await page.keyboard.press("ArrowLeft");
  await expect(releaseTab).toBeFocused();
  await expect(releaseTab).toHaveAttribute("aria-selected", "true");
  await expectQuery(page, {
    "reportTab.project-detail": "release",
    reportEvidence: illustrative.id,
    preserved: "yes",
  });
  await expect(examplePanel).toHaveCount(0);
  await page.keyboard.press("End");
  await expect(exampleTab).toBeFocused();
  await expect(exampleTab).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Home");
  await expect(releaseTab).toBeFocused();
  await page.keyboard.press("ArrowLeft");
  await expect(exampleTab).toBeFocused();
  await expectQuery(page, { "reportTab.project-detail": "example" });
  await page.reload();
  await expect(exampleTab).toHaveAttribute("aria-selected", "true");
  await expect(disclosure).toHaveAttribute("open", "");

  await report.getByRole("button", { name: example.projects[0].title }).click();
  await expectQuery(page, {
    reportProject: release.project_id,
    reportEvidence: null,
    "reportTab.project-detail": "example",
  });
  await expect(exampleTab).toHaveCount(0);
  await expect(releaseTab).toHaveAttribute("aria-selected", "true");
  await report
    .getByRole("button", { name: "All projects", exact: true })
    .click();
  await expect(exampleTab).toHaveAttribute("aria-selected", "true");
  await expect(disclosure).not.toHaveAttribute("open", "");
  await expect(examplePanel).toHaveCount(0);
  await disclosure.locator("summary").focus();
  await page.keyboard.press("Enter");
  await expect(examplePanel).toBeVisible();
  const inspect = examplePanel.getByRole("button", {
    name: "Inspect evidence",
  });
  await inspect.click();
  await expectQuery(page, { reportEvidence: illustrative.id });
  await page.goBack();
  await expect(inspect).toHaveAttribute("aria-expanded", "false");
  await page.goForward();
  await expect(inspect).toHaveAttribute("aria-expanded", "true");

  await report
    .getByRole("combobox", { name: "Filter by freshness" })
    .selectOption("unavailable");
  await expect(report.locator('[data-report-layout="tabs"]')).toHaveCount(0);
  await report
    .getByRole("combobox", { name: "Filter by freshness" })
    .selectOption("all");
  await expect(exampleTab).toHaveAttribute("aria-selected", "true");
  const accessibility = await new AxeBuilder({ page })
    .include('[aria-label="Visual report"]')
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(accessibility.violations).toEqual([]);
  expectReadOnly(state);
});

function liveObservation() {
  return {
    document_ref: `document:${DOC_ID}`,
    revision_ref: "document_revision:dashboard-r1",
    observed_at: OBSERVED_AT.replace(/(?:\.\d+)?Z$/, ".123456789Z"),
    panels: [
      {
        id: "initiatives",
        type: "live-initiatives",
        data: {
          items: [
            {
              ref: "card:launch",
              title: "Launch readiness",
              summary: "The release is ready for the final review.",
              progress: { done: 3, total: 7 },
              priority: "p1",
              phase: "in_progress",
              health: "stale",
              plan: {
                steps: [
                  {
                    id: "review",
                    title: "Finish review",
                    ref: "card:review",
                    after: [],
                  },
                ],
              },
              plan_state: {
                shape: "chain",
                steps: [
                  {
                    id: "review",
                    status: "active",
                    resolvable: true,
                  },
                ],
                next_steps: ["review"],
              },
              assignee_refs: ["actor:agent-reviewer"],
              needs: ["Needs Alex: choose the launch date"],
            },
            {
              ref: "card:onboarding",
              title: "Improve onboarding",
              summary: "Help new teams reach their first useful dashboard.",
              progress: { done: 5, total: 6 },
              priority: "p2",
              phase: "review",
              needs: [],
            },
          ],
        },
      },
      {
        id: "asks",
        type: "live-asks",
        data: {
          items: [
            {
              id: "ask-launch",
              title: "Ship on Friday?",
              status: "open",
              age_seconds: 7200,
            },
            {
              id: "completed:answered-theme",
              title: "Which launch theme?",
              status: "answered",
              age_seconds: 86400,
              response_text: "Lead with executive visibility.",
            },
          ],
        },
      },
      {
        id: "mix",
        type: "live-work-mix",
        data: {
          total: 7,
          group_by: "phase",
          buckets: [
            { key: "in_progress", label: "in progress", count: 4 },
            { key: "review", label: "review", count: 2 },
            { key: "blocked", label: "blocked", count: 1 },
          ],
        },
      },
      {
        id: "owned",
        type: "live-cards",
        data: {
          items: [
            {
              ref: "card:onboarding-copy",
              title: "Onboarding copy pass",
              summary: "Rewrite the empty states.",
              priority: "p2",
              phase: "in_progress",
              board_ref: "board:launch",
              updated_at: OBSERVED_AT,
              needs: [],
            },
          ],
        },
      },
      {
        id: "activity",
        type: "live-activity",
        data: {
          items: [
            {
              ref: "event:decision",
              summary: "Alex answered: lead with executive visibility.",
              ts: OBSERVED_AT,
              count: 1,
            },
            {
              ref: "event:phase",
              summary: "Onboarding moved to review",
              ts: OBSERVED_AT,
              count: 1,
            },
            {
              ref: "event:burst",
              summary: "claude updated Initiatives · 7 edits",
              ts: OBSERVED_AT,
              count: 7,
            },
          ],
        },
      },
    ].map((panel) => ({
      ...panel,
      status: "ok",
      truncated: false,
      observed_at: OBSERVED_AT.replace(/(?:\.\d+)?Z$/, ".123456789Z"),
    })),
  };
}

async function installLiveDashboard(page) {
  const state = await installReportDocument(page, liveDashboardExample);
  state.live = liveObservation();
  state.liveReads = 0;
  state.liveFailure = false;
  await page.route(`**/docs/${DOC_ID}/report`, async (route) => {
    state.liveReads++;
    state.requests.push({
      path: `/docs/${DOC_ID}/report`,
      method: route.request().method(),
    });
    return route.fulfill({
      status: state.liveFailure ? 403 : 200,
      contentType: "application/json",
      body: JSON.stringify(
        state.liveFailure ? { error: { code: "forbidden" } } : state.live,
      ),
    });
  });
  return state;
}

for (const viewport of [
  { width: 1440, height: 1100 },
  { width: 390, height: 844 },
]) {
  test(`mixed live dashboard layout at ${viewport.width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport);
    const state = await installLiveDashboard(page);
    await page.goto(DOC_PATH);
    const report = reportRegion(page);
    await expect(
      report.getByRole("link", { name: "Launch readiness" }),
    ).toBeVisible();
    await expect(report).toContainText("3/7");
    await expect(report).toContainText("Needs Alex: choose the launch date");
    /*
     * The initiative's state, through the one `WorkSummary` the Overview and
     * the Tasks table also render: a badge reading the label rather than this
     * panel's own lowercased copy of the raw state token.
     */
    await expect(
      report.locator("[data-work-summary] [data-health='stale']"),
    ).toContainText("Stale");
    const initiative = report.locator('[data-report-initiative="card:launch"]');
    const initiativePlan = initiative.locator("[data-initiative-plan]");
    await expect(
      initiativePlan.getByRole("link", { name: "Finish review" }),
    ).toHaveAttribute("href", /tasks\/card%3Areview$/i);
    await expect(initiativePlan.getByText("View plan steps")).toBeVisible();
    await expect(report).toContainText("On it: actor:agent-reviewer");
    await expect(report).toContainText("2h old");
    await expect(
      report.getByRole("link", { name: "Which launch theme?" }),
    ).toHaveAttribute(
      "href",
      /mailbox=handled&item=completed%3Aanswered-theme/,
    );
    await expect(report).toContainText("7 open tasks");
    await expect(report).toContainText("This note is an authored snapshot.");
    // The read time is a header chip now, in relative time: every live panel
    // says so where the reader looks before reading it.
    await expect(report.locator("[data-anx-provenance='live']")).toHaveCount(5);
    await expect(
      report.locator("[data-anx-provenance='live']").first(),
    ).toContainText("Live · updated just now");
    await expect(report).not.toContainText("Live as of");
    // And the authored note beside them says who wrote it, and when.
    await expect(
      report.locator("[data-anx-provenance^='authored']"),
    ).toContainText("Written by");
    await expect(report).not.toContainText("No automatic source refresh");
    /*
     * Progress, as the one summary renderer states it. A compact row carries
     * the count rather than a bar, and the sentence is its accessible name:
     * "3/7" alone reads as a date.
     */
    await expect(
      report.getByRole("img", {
        name: "Launch readiness: 3 of 7 steps done",
      }),
    ).toHaveText("3/7");
    const scan = await new AxeBuilder({ page })
      .include('[aria-label="Visual report"]')
      .analyze();
    expect(scan.violations).toEqual([]);
    await expectNoClippedContent(page, "live dashboard");
    await saveScreenshot(
      viewport.width < 600 ? page : report,
      testInfo,
      `live-dashboard-${viewport.width}`,
    );
    expectReadOnly(state);
  });
}

test("live initiative reports keep unresolvable linked work non-navigable", async ({
  page,
}) => {
  const state = await installLiveDashboard(page);
  state.live.panels[0].data.items[0].plan_state.steps[0].resolvable = false;
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  const initiativePlan = report
    .locator('[data-report-initiative="card:launch"]')
    .locator("[data-initiative-plan]");
  const hiddenRef = initiativePlan.locator(
    '.anx-ref-chip[data-anx-ref="card:review"]',
  );
  await expect(hiddenRef).toHaveAttribute("role", "note");
  await expect(hiddenRef).toContainText("not found");
  await expect(
    initiativePlan.getByRole("link", { name: "Finish review" }),
  ).toHaveCount(0);
  expectReadOnly(state);
});

test("Current filter retains RFC3339Nano live panels", async ({ page }) => {
  await installLiveDashboard(page);
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  await expect(
    report.getByRole("link", { name: "Launch readiness" }),
  ).toBeVisible();
  await report
    .getByLabel("Filter by freshness", { exact: true })
    .selectOption("current");
  await expect(
    report.getByRole("link", { name: "Launch readiness" }),
  ).toBeVisible();
  await expect(report).toContainText("3/7");
  await expect(report).toContainText("7 open tasks");
});

test("live panels refresh without changing the saved report and fail safely", async ({
  page,
}) => {
  await page.clock.install({ time: new Date(OBSERVED_AT) });
  const state = await installLiveDashboard(page);
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  await expect(report).toContainText("3/7");
  state.live.panels[0].data.items[0].progress.done = 4;
  state.live.panels[0].truncated = true;
  await page.clock.fastForward(31_000);
  await expect(report).toContainText("4/7");
  await expect(report).toContainText("Partial view.");
  state.liveFailure = true;
  await page.clock.fastForward(31_000);
  await expect(report.getByText(/Live data unavailable/)).toHaveCount(5);
  await expect(report).not.toContainText("4/7");
  await expect(report).toContainText("This note is an authored snapshot.");
  expect(state.liveReads).toBeGreaterThanOrEqual(3);
  expectReadOnly(state);
});

test("live query data is withheld when the report head changed", async ({
  page,
}) => {
  await page.clock.install({ time: new Date(OBSERVED_AT) });
  const state = await installLiveDashboard(page);
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  await expect(report).toContainText("3/7");
  state.live.revision_ref = "document_revision:dashboard-r2";
  await page.clock.fastForward(31_000);
  await expect(report.getByText(/Live data unavailable/)).toHaveCount(5);
  await expect(report).not.toContainText("3/7");
  expectReadOnly(state);
});

test("an unreported metric is an em dash with the reason on hover", async ({
  page,
}) => {
  const example = cloneExample();
  /*
   * A metric strip beside the existing panels: one real reading, and two the
   * producer could not get. The contract makes `value` a string, so an
   * absence arrives as a *word* — which is exactly why it used to wrap
   * through a tile sized for `94%`.
   */
  example.panels.push({
    id: "release-measures",
    project_id: example.projects[0].id,
    type: "metric-strip",
    title: "Release measures",
    author: "claude",
    provenance: "reported",
    observed_at: OBSERVED_AT,
    freshness: "current",
    source_ids: [],
    data: {
      items: [
        { label: "Reviewed", value: "94%", detail: "Of the changed files." },
        { label: "Coverage", value: "unknown", detail: "The read failed." },
        { label: "Latency", value: "n/a", detail: "Not reported this run." },
      ],
    },
  });
  await installReportDocument(page, example);
  await page.goto(DOC_PATH);

  const strip = reportRegion(page).locator(
    "[data-report-panel='release-measures']",
  );
  await expect(strip).toBeVisible();
  const values = strip.locator(".metric-value");
  await expect(values.nth(0)).toHaveText("94%");

  // Not the word "unknown" wrapping through a tile sized for a number.
  const missing = strip.locator("[data-unavailable]");
  await expect(missing).toHaveCount(2);
  await expect(missing.nth(0)).toHaveText("—");
  // A producer that said "unknown" said something; it is quoted back on hover
  // rather than printed where the number goes.
  await expect(missing.nth(0)).toHaveAttribute("data-tooltip", /unknown/);
  await expect(missing.nth(1)).toHaveAttribute("data-tooltip", /n\/a/);
  await expect(
    strip.locator(".metric-value").filter({ hasText: "unknown" }),
  ).toHaveCount(0);

  // And the strip still fits: nothing wrapped out of its box.
  const overflows = await strip.evaluate(
    (node) => node.scrollWidth > node.clientWidth + 1,
  );
  expect(overflows).toBe(false);
});

test("an unreported point in a metric chart reads as a dash, never zero", async ({
  page,
}) => {
  await installReportDocument(page);
  await page.goto(DOC_PATH);
  const chart = reportRegion(page).locator(
    "[data-report-panel='example-metrics']",
  );
  await expect(chart).toBeVisible();
  const missing = chart.locator(".report-chart [data-unavailable]");
  await expect(missing).toHaveCount(1);
  await expect(missing).toHaveText("—");
  await expect(missing).toHaveAttribute("data-tooltip", /not the same as zero/);
  await expect(chart).toContainText("— means unavailable, never zero.");
});
