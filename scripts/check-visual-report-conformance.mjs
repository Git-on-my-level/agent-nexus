import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { parseVisualReport } from "../web-ui/src/lib/visualReports.js";
import { validateLiveQuery } from "../web-ui/src/lib/liveReports.js";

const fixtures = new URL(
  "../contracts/fixtures/visual-reports/",
  import.meta.url,
);
const cases = JSON.parse(
  readFileSync(new URL("reports.json", fixtures), "utf8"),
);
const run = spawnSync("go", ["run", "./cmd/conformance"], {
  cwd: fileURLToPath(new URL("../contracts/visualreport/", import.meta.url)),
  input: JSON.stringify(cases.map((item) => item.content)),
  encoding: "utf8",
  maxBuffer: 8 * 1024 * 1024,
});
if (run.status !== 0)
  throw new Error(run.stderr || run.error || "Go validator failed");
const go = JSON.parse(run.stdout);
if (go.length !== cases.length) throw new Error("Go validator omitted cases");
const failures = [];
for (const [i, item] of cases.entries()) {
  const parsed = parseVisualReport(item.content);
  const js = { recognized: parsed.recognized, valid: Boolean(parsed.report) };
  if (
    js.recognized !== item.recognized ||
    js.valid !== item.valid ||
    go[i].recognized !== js.recognized ||
    go[i].valid !== js.valid
  )
    failures.push(
      `${item.name}: expected=${item.recognized}/${item.valid} JS=${js.recognized}/${js.valid} Go=${go[i].recognized}/${go[i].valid}`,
    );
}
const queries = JSON.parse(
  readFileSync(new URL("queries.json", fixtures), "utf8"),
);
for (const item of queries) {
  if ((validateLiveQuery(item.type, item.data).length === 0) !== item.valid)
    failures.push(`query ${item.name}: browser differs from corpus`);
}
if (failures.length) throw new Error(failures.join("\n"));
console.log(
  `Visual-report conformance: ${cases.length} reports and ${queries.length} queries agree.`,
);
