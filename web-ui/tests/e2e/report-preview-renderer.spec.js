import { execFile } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import { runReportRenderer } from "./helpers/report-renderer.js";

const execFileAsync = promisify(execFile);
const renderer = new URL(
  "../../scripts/preview-visual-report.mjs",
  import.meta.url,
);
const webRoot = fileURLToPath(new URL("../..", import.meta.url));

test("headless report preview writes a PNG from injected live observations", async ({
  page,
}, testInfo) => {
  test.setTimeout(120_000);
  expect(page).toBeTruthy();
  const reportPath = testInfo.outputPath("preview-report.json");
  const observationsPath = testInfo.outputPath("preview-observations.json");
  const outputPath = testInfo.outputPath("preview.png");
  await mkdir(dirname(reportPath), { recursive: true });
  await Promise.all([
    writeFile(
      reportPath,
      JSON.stringify({
        kind: "anx.visual-report",
        schema_version: 1,
        title: "Preview renderer smoke test",
        summary: "A live report rendered without saving a document.",
        generated_at: "2026-10-05T00:00:00Z",
        projects: [
          {
            id: "workspace",
            title: "Workspace",
            summary: "Current movement",
            outcome: "See live data",
          },
        ],
        sources: [],
        panels: [
          {
            id: "movement",
            project_id: "workspace",
            type: "live-activity",
            title: "Recent movement",
            author: "test",
            provenance: "reported",
            observed_at: null,
            freshness: "unknown",
            source_ids: [],
            data: { limit: 1 },
          },
          {
            id: "throughput",
            project_id: "workspace",
            type: "chart",
            title: "Merged PR throughput",
            author: "test",
            provenance: "reported",
            observed_at: null,
            freshness: "unknown",
            source_ids: [],
            data: {},
            source: {
              series: "github-prs",
              labels: { status: "merged" },
              range: "84d",
              agg: "sum",
            },
          },
        ],
      }),
      "utf8",
    ),
    writeFile(
      observationsPath,
      JSON.stringify([
        {
          id: "movement",
          type: "live-activity",
          status: "ok",
          observed_at: "2026-10-05T00:00:00Z",
          data: { items: [{ summary: "Release checklist moved forward" }] },
        },
        {
          id: "throughput",
          type: "chart",
          status: "ok",
          observed_at: "2026-10-05T00:00:00Z",
          truncated: false,
          provenance: {
            adapter: "github",
            host: "collector",
            last_push: "2026-10-05T00:00:00Z",
            resolution: "raw",
          },
          data: {
            option: {
              xAxis: { type: "time" },
              yAxis: { type: "value", name: "PRs" },
              series: [
                {
                  name: "github-prs initiative:sca-612 status=merged",
                  type: "line",
                  data: [[1791158400000, 3]],
                },
              ],
            },
          },
        },
      ]),
      "utf8",
    ),
  ]);

  await runReportRenderer(
    process.execPath,
    [
      renderer.pathname,
      "--report",
      reportPath,
      "--observations",
      observationsPath,
      "--output",
      outputPath,
      "--expect-text",
      "Release checklist moved forward",
    ],
    { cwd: webRoot, timeout: 100_000 },
    outputPath,
  );

  const png = await readFile(outputPath);
  expect(png.subarray(0, 8)).toEqual(
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
  );
  expect(png.length).toBeGreaterThan(500);
});

// The Playwright test API requires fixture destructuring, even when none are needed.
// eslint-disable-next-line no-empty-pattern
test("sandbox launch failure returns a machine-readable text fallback", async ({}, testInfo) => {
  const reportPath = testInfo.outputPath("fallback-report.json");
  const observationsPath = testInfo.outputPath("fallback-observations.json");
  const outputPath = testInfo.outputPath("fallback.png");
  await mkdir(dirname(reportPath), { recursive: true });
  await Promise.all([
    writeFile(reportPath, JSON.stringify({ title: "Fallback report" }), "utf8"),
    writeFile(observationsPath, "[]", "utf8"),
  ]);

  const driver = `
    import { run } from ${JSON.stringify(renderer.href)};
    const exitCode = await run(${JSON.stringify([
      "--report",
      reportPath,
      "--observations",
      observationsPath,
      "--output",
      outputPath,
    ])}, {
      launchBrowser: async (options) => {
        if (options.channel !== "chromium" || options.headless !== true)
          throw new Error("preview launch did not use Chromium's new headless mode");
        if (options.chromiumSandbox !== true)
          throw new Error("preview launch did not require Chromium sandboxing");
        throw new Error("Chromium sandboxing failed!");
      },
    });
    process.exitCode = exitCode;
  `;
  const { stdout } = await execFileAsync(
    process.execPath,
    ["--input-type=module", "--eval", driver],
    { cwd: webRoot, timeout: 30_000 },
  );

  expect(JSON.parse(stdout)).toEqual({
    rendered: false,
    reason: "sandbox_unavailable",
  });
  let outputCreated = true;
  try {
    await readFile(outputPath);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    outputCreated = false;
  }
  expect(outputCreated).toBe(false);
});
