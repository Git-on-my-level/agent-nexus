import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname } from "node:path";

import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

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
    await saveScreenshot(report, testInfo, `report-overview-${viewport.width}`);
    // The ANX shell owns the scroll container, not necessarily window.
    // Restore the actual first screen after full-height component capture.
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
    await saveScreenshot(
      evidenceRegion,
      testInfo,
      `report-evidence-${viewport.width}`,
    );

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
  await expect(panelRegion(report, explanation)).toContainText(
    explanation.data.text,
  );
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
