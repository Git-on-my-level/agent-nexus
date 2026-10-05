import { execFile } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

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

  await execFileAsync(
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
  );

  const png = await readFile(outputPath);
  expect(png.subarray(0, 8)).toEqual(
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
  );
  expect(png.length).toBeGreaterThan(500);
});
