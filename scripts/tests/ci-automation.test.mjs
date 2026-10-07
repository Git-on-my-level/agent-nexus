import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { balance, discoveredFiles, fileFilter } from "../ci-e2e.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const ci = readFileSync(join(root, ".github/workflows/ci.yml"), "utf8");
const gate = ci
  .slice(ci.indexOf("\n  ci-ok:") + 1)
  .split(/\n(?=  [\w-]+:\n)/)[0];
const jobs = [
  ...ci.slice(ci.indexOf("\njobs:")).matchAll(/^  ([\w-]+):$/gm),
].map((match) => match[1]);
const needs = [...gate.matchAll(/^      - ([\w-]+)$/gm)].map(
  (match) => match[1],
);

test("ci-ok depends on every other job and runs even after failures", () => {
  assert.deepEqual(
    [...needs].sort(),
    jobs.filter((job) => job !== "ci-ok").sort(),
  );
  assert.match(gate, /if: always\(\)/);
  assert.match(ci, /merge_group:\s+types: \[checks_requested\]/);
  const smokes = readFileSync(
    join(root, ".github/workflows/system-smokes.yml"),
    "utf8",
  );
  assert.match(smokes, /merge_group:\s+types: \[checks_requested\]/);
});

test("the actual aggregate shell gate accepts path skips and rejects failure or cancellation in any job", () => {
  const script = gate
    .split("        run: |\n")[1]
    .split("\n")
    .map((line) => line.slice(10))
    .join("\n");
  function check(results) {
    const result = spawnSync("bash", ["-eo", "pipefail", "-c", script], {
      env: { ...process.env, NEEDS_JSON: JSON.stringify(results) },
      encoding: "utf8",
    });
    assert.ifError(result.error);
    return result.status;
  }
  const passing = Object.fromEntries(
    needs.map((job) => [job, { result: "success" }]),
  );
  assert.equal(check(passing), 0);
  const filtered = Object.fromEntries(
    needs.map((job) => [
      job,
      { result: job === "changes" ? "success" : "skipped" },
    ]),
  );
  assert.equal(check(filtered), 0);
  for (const job of needs) {
    for (const result of ["failure", "cancelled", "pending", ""]) {
      assert.notEqual(
        check({ ...passing, [job]: { result } }),
        0,
        `${job}: ${result}`,
      );
    }
  }
  assert.notEqual(check({}), 0);
  assert.notEqual(check({ ...passing, changes: { result: "skipped" } }), 0);
});

test("balancing includes untimed specs, is deterministic, and rejects empty plans", () => {
  const files = new Map([
    ["slow.spec.js", 1],
    ["medium.spec.js", 2],
    ["new.spec.js", 3],
    ["fast.spec.js", 1],
  ]);
  const timings = {
    "slow.spec.js": 10000,
    "medium.spec.js": 6000,
    "fast.spec.js": 2000,
    "deleted.spec.js": 90000,
  };
  const plan = balance(files, timings, 2);
  assert.deepEqual(plan, balance(new Map([...files].reverse()), timings, 2));
  assert.deepEqual(
    plan.flatMap((shard) => shard.files).sort(),
    [...files.keys()].sort(),
  );
  assert.equal(
    plan.reduce((sum, shard) => sum + shard.tests, 0),
    7,
  );
  assert.throws(() => balance(files, timings, 5));
  assert.throws(() => balance(files, timings, 0));
  assert.throws(() => discoveredFiles({ suites: [], errors: [] }));
  assert.throws(() =>
    discoveredFiles({ suites: [], errors: [{ message: "broken spec" }] }),
  );
  const filter = new RegExp(fileFilter("a+[1].spec.js"));
  assert.ok(filter.test("/repo/tests/e2e/a+[1].spec.js"));
  assert.ok(!filter.test("/repo/tests/e2e/extra-a+[1].spec.js"));
});

test("all balanced shards cover exactly the real Playwright suite, including base-path, without overlap", () => {
  const require = createRequire(join(root, "web-ui/package.json"));
  const cli = join(
    dirname(require.resolve("@playwright/test/package.json")),
    "cli.js",
  );
  function discover(filters = []) {
    const env = { ...process.env };
    for (const key of Object.keys(env))
      if (key.startsWith("PLAYWRIGHT_JSON_OUTPUT_")) delete env[key];
    const result = spawnSync(
      process.execPath,
      [cli, "test", "--list", "--reporter=json", ...filters],
      {
        cwd: join(root, "web-ui"),
        env,
        encoding: "utf8",
        maxBuffer: 32 * 1024 * 1024,
      },
    );
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
    return JSON.parse(result.stdout);
  }
  function ids(report) {
    const found = [];
    function visit(suite) {
      for (const spec of suite.specs ?? []) {
        for (const test of spec.tests)
          found.push(`${spec.id}:${test.projectName}`);
      }
      for (const child of suite.suites ?? []) visit(child);
    }
    for (const suite of report.suites) visit(suite);
    return found;
  }
  const report = discover();
  const timings = JSON.parse(
    readFileSync(join(root, ".github/e2e-timings.json"), "utf8"),
  );
  const browserJob = ci
    .slice(ci.indexOf("\n  web-ui-e2e-check:") + 1)
    .split(/\n(?=  [\w-]+:\n)/)[0];
  const count = browserJob.match(/shard: \[([^\]]+)\]/)[1].split(",").length;
  const plan = balance(discoveredFiles(report), timings.files, count);
  const expected = ids(report).sort();
  const actual = plan
    .flatMap((shard) => ids(discover(shard.files.map(fileFilter))))
    .sort();
  assert.ok(expected.some((id) => id.endsWith(":base-path")));
  assert.equal(new Set(actual).size, actual.length);
  assert.deepEqual(actual, expected);
});

test("all four route shards, legacy checks and executed coverage are required", () => {
  const job = (name) =>
    ci.slice(ci.indexOf(`\n  ${name}:`) + 1).split(/\n(?=  [\w-]+:\n)/)[0];
  const routes = job("core-performance-routes");
  assert.match(routes, /shard: \[1, 2, 3, 4\]/);
  assert.match(routes, /fail-fast: false/);
  assert.match(routes, /ANX_PERFORMANCE_SHARD: \$\{\{ matrix.shard \}\}/);
  assert.match(routes, /if-no-files-found: error/);
  assert.match(
    routes,
    /name: core-performance-routes-\$\{\{ matrix.shard \}\}/,
  );
  assert.match(
    job("core-performance-coverage"),
    /needs: \[changes, core-performance-routes\]/,
  );
  assert.match(job("core-performance-coverage"), /check-performance-shards.py/);
  assert.match(
    job("core-performance-legacy"),
    /-skip '\^TestPerformanceRoutes\$'/,
  );
  for (const name of [
    "core-performance-routes",
    "core-performance-legacy",
    "core-performance-coverage",
  ]) {
    assert.ok(needs.includes(name));
    assert.doesNotMatch(job(name), /continue-on-error/);
    for (const flag of ["core", "contracts", "go_shared"])
      assert.ok(job(name).includes(`needs.changes.outputs.${flag} == 'true'`));
  }
});
