import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import { runReportRenderer } from "./helpers/report-renderer.js";

const renderer = new URL(
  "../../scripts/preview-visual-report.mjs",
  import.meta.url,
);
const webRoot = fileURLToPath(new URL("../..", import.meta.url));
const fixtureRoot = fileURLToPath(
  new URL("../fixtures/report-templates/", import.meta.url),
);
const templateNames = [
  "workspace-overview",
  "initiative",
  "weekly-review",
  "release-readiness",
  "incident-review",
  "fleet-health",
];
const fixtureText = {
  "workspace-overview": ["Launch", "Launch checklist updated"],
  initiative: [
    "Launch",
    "Finish review",
    "Ship launch",
    "actor:agent-reviewer",
    "Approve the launch window",
  ],
  "weekly-review": ["Launch checklist updated", "Launch"],
  "release-readiness": ["Launch", "Choose the release date"],
  "incident-review": ["Launch checklist updated", "Choose the release date"],
  "fleet-health": ["Collector"],
};

test("browser renders all six report templates against live fixtures", async ({
  request,
}, testInfo) => {
  void request;
  test.setTimeout(300_000);
  for (const name of templateNames) {
    const fixture = JSON.parse(
      await readFile(join(fixtureRoot, name + ".json"), "utf8"),
    );
    const reportPath = testInfo.outputPath(name + "-report.json");
    const observationsPath = testInfo.outputPath(name + "-observations.json");
    const outputPath = testInfo.outputPath(name + ".png");
    await mkdir(dirname(reportPath), { recursive: true });
    await Promise.all([
      writeFile(reportPath, JSON.stringify(fixture.report), "utf8"),
      writeFile(observationsPath, JSON.stringify(fixture.observations), "utf8"),
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
        ...fixtureText[name].flatMap((text) => ["--expect-text", text]),
      ],
      { cwd: webRoot, timeout: 100_000 },
      outputPath,
    );
    const png = await readFile(outputPath);
    expect(png.subarray(0, 8), name).toEqual(
      Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    );
    expect(png.length, name).toBeGreaterThan(500);
  }
});
