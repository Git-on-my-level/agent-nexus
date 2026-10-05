import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

export function discoveredFiles(report) {
  const files = new Map();
  function visit(suite) {
    for (const spec of suite.specs ?? []) {
      files.set(spec.file, (files.get(spec.file) ?? 0) + spec.tests.length);
    }
    for (const child of suite.suites ?? []) visit(child);
  }
  for (const suite of report.suites ?? []) visit(suite);
  if (report.errors?.length || files.size === 0) {
    throw new Error("Playwright discovery failed or found no tests");
  }
  return files;
}

// Longest-first scheduling, with lexical ties, keeps the plan identical in every
// matrix job. Discover from Playwright instead of using the timing file as an
// allowlist: new specs and tests must run even before they have measurements.
export function balance(files, timings, count) {
  if (!Number.isSafeInteger(count) || count < 1 || count > files.size) {
    throw new Error(
      "Shard count must be between 1 and the number of spec files",
    );
  }
  const measured = [...files.keys()].filter((file) => timings[file] > 0);
  const perTest = measured
    .map((file) => timings[file] / files.get(file))
    .sort((a, b) => a - b);
  const fallback = perTest[Math.floor(perTest.length / 2)] ?? 2000;
  const weighted = [...files].map(([file, tests]) => ({
    file,
    tests,
    duration: timings[file] > 0 ? timings[file] : tests * fallback,
  }));
  weighted.sort(
    (a, b) =>
      b.duration - a.duration ||
      (a.file < b.file ? -1 : a.file > b.file ? 1 : 0),
  );
  const shards = Array.from({ length: count }, () => ({
    files: [],
    tests: 0,
    duration: 0,
  }));
  for (const spec of weighted) {
    const shard = shards.reduce((best, next) =>
      next.duration < best.duration ? next : best,
    );
    shard.files.push(spec.file);
    shard.tests += spec.tests;
    shard.duration += spec.duration;
  }
  return shards;
}

export function fileFilter(file) {
  return "(?:^|/)" + file.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "$";
}

function main() {
  const [selection, option] = process.argv.slice(2);
  const match = /^(\d+)\/(\d+)$/.exec(selection ?? "");
  if (!match || (option && option !== "--plan")) {
    throw new Error("Usage: node scripts/ci-e2e.mjs INDEX/COUNT [--plan]");
  }
  const [index, count] = match.slice(1).map(Number);
  if (index < 1 || index > count) throw new Error("Invalid shard index");
  const require = createRequire(join(root, "web-ui/package.json"));
  const cli = join(
    dirname(require.resolve("@playwright/test/package.json")),
    "cli.js",
  );
  const env = { ...process.env };
  // A configured JSON output file would divert discovery away from stdout.
  delete env.PLAYWRIGHT_JSON_OUTPUT_FILE;
  delete env.PLAYWRIGHT_JSON_OUTPUT_DIR;
  delete env.PLAYWRIGHT_JSON_OUTPUT_NAME;
  const discovery = spawnSync(
    process.execPath,
    [cli, "test", "--list", "--reporter=json"],
    {
      cwd: join(root, "web-ui"),
      env,
      encoding: "utf8",
      maxBuffer: 32 * 1024 * 1024,
    },
  );
  if (discovery.error || discovery.status !== 0) {
    throw new Error(
      `Playwright discovery failed: ${discovery.error ?? discovery.stderr}`,
    );
  }
  const timings = JSON.parse(
    readFileSync(join(root, ".github/e2e-timings.json"), "utf8"),
  );
  const plan = balance(
    discoveredFiles(JSON.parse(discovery.stdout)),
    timings.files,
    count,
  );
  console.log(
    JSON.stringify(
      plan.map((shard, i) => ({ shard: i + 1, ...shard })),
      null,
      2,
    ),
  );
  if (option === "--plan") return;
  const result = spawnSync(
    process.execPath,
    [
      cli,
      "test",
      ...plan[index - 1].files.map(fileFilter),
      "--retries=0",
      "--trace=retain-on-failure",
      "--reporter=list,json",
      "--output=test-results/e2e",
    ],
    { cwd: join(root, "web-ui"), stdio: "inherit" },
  );
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  main();
