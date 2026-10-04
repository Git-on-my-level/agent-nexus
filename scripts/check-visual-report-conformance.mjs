import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { parseVisualReport } from "../web-ui/src/lib/visualReports.js";
import { validateLiveQuery } from "../web-ui/src/lib/liveReports.js";
import { countMarkdownTaskProgress } from "../web-ui/src/lib/markdown.js";

const fixtures = new URL(
  "../contracts/fixtures/visual-reports/",
  import.meta.url,
);
const cases = JSON.parse(
  readFileSync(new URL("reports.json", fixtures), "utf8"),
);
const summaries = JSON.parse(
  readFileSync(new URL("summaries.json", fixtures), "utf8"),
);
const run = spawnSync("go", ["run", "./cmd/conformance"], {
  cwd: fileURLToPath(new URL("../contracts/visualreport/", import.meta.url)),
  input: JSON.stringify({
    reports: cases.map((item) => item.content),
    summaries: summaries.map((item) => item.markdown),
  }),
  encoding: "utf8",
  maxBuffer: 8 * 1024 * 1024,
});
if (run.status !== 0)
  throw new Error(run.stderr || run.error || "Go validator failed");
const go = JSON.parse(run.stdout);
if (go.reports.length !== cases.length)
  throw new Error("Go validator omitted report cases");
if (go.summaries.length !== summaries.length)
  throw new Error("Go parser omitted summary cases");
const failures = [];
for (const [i, item] of cases.entries()) {
  const parsed = parseVisualReport(item.content);
  const js = { recognized: parsed.recognized, valid: Boolean(parsed.report) };
  const goReport = go.reports[i];
  const expectedGoValid = item.go_valid ?? item.valid;
  const hasSourceUrl = (() => {
    try {
      const report = JSON.parse(item.content);
      return report.sources?.some((source) => typeof source.url === "string");
    } catch {
      return false;
    }
  })();
  const directionalURL = hasSourceUrl && item.go_valid !== undefined;
  // URL fixtures may document an intentional stricter Go rejection. Every
  // URL still enforces Go-accept => JS-accept; plain cases keep exact parity.
  if (
    js.recognized !== item.recognized ||
    js.valid !== item.valid ||
    goReport.recognized !== js.recognized ||
    goReport.valid !== expectedGoValid ||
    (hasSourceUrl && goReport.valid && !js.valid) ||
    (!directionalURL && goReport.valid !== js.valid)
  )
    failures.push(
      `${item.name}: expected JS=${item.recognized}/${item.valid} Go=${expectedGoValid} JS=${js.recognized}/${js.valid} Go=${goReport.recognized}/${goReport.valid}`,
    );
}
const queries = JSON.parse(
  readFileSync(new URL("queries.json", fixtures), "utf8"),
);
for (const item of queries) {
  if ((validateLiveQuery(item.type, item.data).length === 0) !== item.valid)
    failures.push(`query ${item.name}: browser differs from corpus`);
}
for (const [i, item] of summaries.entries()) {
  const jsProgress = countMarkdownTaskProgress(item.markdown);
  const goProgress = go.summaries[i];
  if (
    jsProgress.done !== item.progress.done ||
    jsProgress.total !== item.progress.total ||
    jsProgress.done !== goProgress.done ||
    jsProgress.total !== goProgress.total
  )
    failures.push(
      `summary ${item.name}: expected=${item.progress.done}/${item.progress.total} JS=${jsProgress.done}/${jsProgress.total} Go=${goProgress.done}/${goProgress.total}`,
    );
}
if (failures.length) throw new Error(failures.join("\n"));
console.log(
  `Visual-report conformance: ${cases.length} reports, ${queries.length} queries, and ${summaries.length} summaries agree.`,
);
