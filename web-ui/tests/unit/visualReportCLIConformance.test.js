import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

import { parseVisualReport } from "../../src/lib/visualReports.js";

const repoRoot = resolve(
  dirname(new URL(import.meta.url).pathname),
  "../../..",
);
const cliDir = resolve(repoRoot, "cli");
const fixtureDir = resolve(repoRoot, "web-ui/src/lib/fixtures");

let tempDir;
let anxBinary;
let cliEnv;

function collectReports(value, reports, seen = new Set()) {
  if (
    typeof value === "string" &&
    value.includes('"kind"') &&
    value.includes("anx.visual-report")
  ) {
    try {
      collectReports(JSON.parse(value), reports, seen);
    } catch {
      /* Ignore unrelated fixture prose. */
    }
    return;
  }
  if (!value || typeof value !== "object" || seen.has(value)) return;
  seen.add(value);
  if (value.kind === "anx.visual-report") {
    reports.push(value);
    return;
  }
  for (const child of Array.isArray(value) ? value : Object.values(value)) {
    collectReports(child, reports, seen);
  }
}

async function webUIReportFixtures() {
  const files = readdirSync(fixtureDir, { recursive: true })
    .filter((file) => String(file).endsWith(".js"))
    .map((file) => join(fixtureDir, file));
  const reports = [];
  for (const file of files) {
    const module = await import(pathToFileURL(file).href);
    collectReports(module, reports);
  }
  const { getGameDevStudioSeedData } =
    await import("../../scripts/game-dev-studio-seed-data.mjs");
  collectReports(getGameDevStudioSeedData(), reports);
  const unique = new Map(
    reports.map((report) => [JSON.stringify(report), report]),
  );
  return [...unique.values()];
}

function runReportValidate(content) {
  return spawnSync(anxBinary, ["--json", "report", "validate", "-"], {
    cwd: repoRoot,
    input: content,
    encoding: "utf8",
    env: cliEnv,
  });
}

function parseCLIEnvelope(cli, label) {
  try {
    return JSON.parse(cli.stdout);
  } catch {
    throw new Error(
      `CLI did not return JSON for ${label}: ${cli.stderr ?? ""}${cli.stdout ?? ""}`,
    );
  }
}

// Real-binary cross-module integration stays in the full CI unit job.
const integration =
  process.env.ANX_TEST_FAST === "1" ? describe.skip : describe;
integration("Go CLI and web visual report validator conformance", () => {
  beforeAll(() => {
    tempDir = mkdtempSync(join(tmpdir(), "anx-report-conformance-"));
    const configDir = join(tempDir, "config");
    const homeDir = join(tempDir, "home");
    mkdirSync(configDir);
    mkdirSync(homeDir);
    // report validate resolves a workspace before it validates. An empty config
    // dir and home keep enrolled hosts off the developer's ~/.config/anx.
    // Drop an inherited base URL so that selection cannot skip the catalog.
    cliEnv = {
      ...process.env,
      HOME: homeDir,
      ANX_CONFIG_DIR: configDir,
    };
    delete cliEnv.ANX_BASE_URL;
    anxBinary = join(tempDir, "anx");
    execFileSync("go", ["build", "-o", anxBinary, "./cmd/anx"], {
      cwd: cliDir,
      stdio: "pipe",
    });
  }, 120_000);

  afterAll(() => {
    if (tempDir) rmSync(tempDir, { recursive: true, force: true });
  });

  it("accepts every report fixture found under web-ui", async () => {
    const reports = await webUIReportFixtures();
    expect(reports.length).toBeGreaterThan(0);

    for (const [index, report] of reports.entries()) {
      const content = JSON.stringify(report);
      const web = parseVisualReport(content);
      const cli = runReportValidate(content);
      const envelope = parseCLIEnvelope(cli, `fixture ${index}`);
      expect(
        { exitCode: cli.status, valid: envelope.ok && envelope.result?.valid },
        `fixture ${index}: ${web.errors.join("; ")} / ${JSON.stringify(envelope.error)}`,
      ).toEqual({ exitCode: 0, valid: Boolean(web.report) });
    }
  }, 120_000);

  it("rejects the same invalid schema and layout examples as the renderer", async () => {
    const reports = await webUIReportFixtures();
    const [base] = reports;
    const withPanel = (type) => {
      const report = reports.find((candidate) =>
        candidate.panels.some((panel) => panel.type === type),
      );
      expect(report, `fixture panel type ${type}`).toBeDefined();
      return report;
    };
    const cases = [
      {
        name: "version",
        source: base,
        change: (report) => {
          report.schema_version = 2;
        },
      },
      {
        name: "unknown field",
        source: base,
        change: (report) => {
          report.unexpected = true;
        },
      },
      {
        name: "panel type",
        source: base,
        change: (report) => {
          report.panels[0].type = "unknown-panel";
        },
      },
      {
        name: "timestamp",
        source: base,
        change: (report) => {
          report.generated_at = "yesterday";
        },
      },
      {
        name: "source reference",
        source: base,
        change: (report) => {
          report.panels[0].source_ids = ["missing-source"];
        },
      },
      {
        name: "evidence table row width",
        source: withPanel("evidence-table"),
        change: (report) => {
          const panel = report.panels.find(
            (item) => item.type === "evidence-table",
          );
          panel.data.rows[0].cells.push("unexpected cell");
        },
      },
      {
        name: "milestone timestamp",
        source: withPanel("milestone-timeline"),
        change: (report) => {
          report.panels.find(
            (item) => item.type === "milestone-timeline",
          ).data.items[0].date = "tomorrow";
        },
      },
      {
        name: "dependency reference",
        source: withPanel("dependency-diagram"),
        change: (report) => {
          report.panels.find(
            (item) => item.type === "dependency-diagram",
          ).data.edges[0].from = "missing-node";
        },
      },
      {
        name: "metric range",
        source: withPanel("metric-chart"),
        change: (report) => {
          report.panels.find(
            (item) => item.type === "metric-chart",
          ).data.points[0].value = -1;
        },
      },
      {
        name: "chart vocabulary",
        source: withPanel("chart"),
        change: (report) => {
          report.panels.find(
            (item) => item.type === "chart",
          ).data.option.series[0].type = "unknown-chart";
        },
      },
      {
        name: "layout reference",
        source: base,
        change: (report) => {
          report.layout = { type: "panel", panel_id: "missing" };
        },
      },
    ];
    for (const { name, source, change } of cases) {
      const report = structuredClone(source);
      change(report);
      const content = JSON.stringify(report);
      const web = parseVisualReport(content);
      const cli = runReportValidate(content);
      const envelope = parseCLIEnvelope(cli, `case ${name}`);
      expect(web.report, `web case ${name}`).toBeNull();
      expect(
        cli.status,
        `CLI case ${name}: ${JSON.stringify(envelope)}`,
      ).not.toBe(0);
      const validationErrors = envelope.error?.details?.errors;
      expect(
        validationErrors,
        `CLI case ${name} was not a validation error: ${JSON.stringify(envelope)}`,
      ).toEqual(expect.any(Array));
      expect(
        validationErrors.length,
        `CLI case ${name}: ${JSON.stringify(envelope)}`,
      ).toBeGreaterThan(0);
    }
  }, 120_000);
});
