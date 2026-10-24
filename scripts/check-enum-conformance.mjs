// Fails when core, the CLI or web-ui disagree with the enums in
// contracts/anx-schema.yaml: the generated constants must match the schema, no
// module may carry its own hand-written copy of a shared value list, and each
// module's conformance test must pass against the generated values.
//
// --copies-only runs just the hand-written-copy guard against the committed
// generated values. It needs only Node, so CI and the commit hook run it on
// every change; the full check runs from scripts/contract-check.
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import * as generated from "../contracts/gen/ts/dist/enums.js";

const root = fileURLToPath(new URL("..", import.meta.url));
const failures = [];
const copiesOnly = process.argv.includes("--copies-only");

function run(cmd, args, cwd) {
  const result = spawnSync(cmd, args, {
    cwd: `${root}${cwd}`,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
  });
  if (result.status !== 0)
    failures.push(
      `${cwd}: ${cmd} ${args.join(" ")} failed\n${result.stdout}${result.stderr}`,
    );
  return result.stdout;
}

// 1. Generated constants match the schema, in contract order.
const schema = copiesOnly
  ? Object.entries(generated)
      .filter(([key]) => key.endsWith("_VALUES"))
      .map(([key, values]) => ({
        name: key.slice(0, -"_VALUES".length).toLowerCase(),
        values,
      }))
  : JSON.parse(
      run("go", ["run", "./cmd/contract-gen", "--print-enums"], "core") || "[]",
    );
if (!schema.length) failures.push("no schema enums to check");
for (const item of copiesOnly ? [] : schema) {
  const name = item.name.toUpperCase();
  const values = generated[`${name}_VALUES`];
  if (JSON.stringify(values) !== JSON.stringify(item.values))
    failures.push(
      `${item.name}: generated ${JSON.stringify(values)} != schema ${JSON.stringify(item.values)}; run make contract-gen`,
    );
  const aliases = generated[`${name}_ALIASES`];
  if (JSON.stringify(aliases ?? null) !== JSON.stringify(item.aliases ?? null))
    failures.push(`${item.name}: generated aliases differ from the schema`);
}

// 2. No hand-written copies of the shared lists. A line quoting most of an
// enum's values is a copy that will drift (the CLI once kept four of six ask
// outcomes). Intentional subsets are listed here with their reason.
const GUARDED = [
  "human_attention_response_outcome",
  "board_column_key",
  "priority",
];
const ALLOWED = [
  {
    file: "core/internal/primitives/brief_facts.go",
    contains: "const briefPrioritySQL",
    reason:
      "backs an expression index; pinned by TestPrioritySQLMatchesContract",
  },
  {
    file: "core/internal/primitives/inbox_reads.go",
    contains: "const inboxAskPrioritySQL",
    reason: "SQL ranking; pinned by TestPrioritySQLMatchesContract",
  },
  {
    file: "core/internal/primitives/work_store.go",
    contains: '"cancelled", "unknown"',
    reason: "work phase, a superset of board columns",
  },
  {
    file: "core/internal/primitives/brief.go",
    contains: 'case "backlog", "ready", "in_progress", "review", "done":',
    reason: "placements that are not blocked",
  },
  {
    file: "core/internal/primitives/moot.go",
    contains: 'case "backlog", "ready", "in_progress", "review":',
    reason: "open, unblocked columns",
  },
  {
    file: "core/internal/pm/decisions.go",
    contains: 'case "backlog", "ready", "in_progress", "blocked", "review":',
    reason: "columns that are not done",
  },
  {
    file: "core/internal/server/work_handlers.go",
    contains: '"cancelled", "unknown"}',
    reason: "work phases, a superset of board columns",
  },
  {
    file: "core/internal/storage/work_summary_indexes.go",
    contains: "'cancelled')",
    reason: "work phases in frozen migration SQL",
  },
  {
    file: "core/internal/storage/agent_summary_indexes.go",
    contains: "idx_brief_asks_open_page ON brief_asks",
    reason: "frozen migration DDL; must equal briefPrioritySQL",
  },
  {
    file: "core/internal/storage/pm_workspace_context.go",
    contains: "CASE column_key WHEN 'blocked' THEN 0",
    reason: "attention order over open columns in frozen migration DDL",
  },
  {
    file: "web-ui/src/lib/brief.js",
    contains: "export const PLACEMENTS",
    reason: "placements that are not blocked",
  },
  {
    file: "web-ui/src/lib/components/pm/WorkViews.svelte",
    contains: '["backlog", "in_progress", "blocked", "review", "done"]',
    reason: "work phases always shown as groups",
  },
];
const members = new Map(
  schema
    .filter((item) => GUARDED.includes(item.name))
    .map((item) => [item.name, new Set(item.values)]),
);
for (const name of GUARDED)
  if (!members.has(name)) failures.push(`schema has no ${name} enum`);
const sources = run(
  "git",
  [
    "ls-files",
    "--cached",
    "--others",
    "--exclude-standard",
    "core",
    "cli",
    "web-ui/src",
  ],
  "",
)
  .split("\n")
  .filter(
    (file) =>
      /\.(go|js|ts|svelte)$/.test(file) &&
      !/(_test\.go|\.test\.js|\.d\.ts)$/.test(file) &&
      !/(^|\/)(__tests__|generated|testdata)\//.test(file),
  );
const quoted = /["'`]([a-z0-9_]+)["'`]/g;
const used = new Set();
for (const file of sources) {
  let text;
  try {
    text = readFileSync(`${root}${file}`, "utf8");
  } catch {
    continue;
  }
  text.split("\n").forEach((line, index) => {
    const tokens = new Set([...line.matchAll(quoted)].map((m) => m[1]));
    for (const [name, values] of members) {
      const hits = [...values].filter((value) => tokens.has(value)).length;
      if (hits < Math.max(3, values.size - 2)) continue;
      const allowed = ALLOWED.findIndex(
        (entry) => entry.file === file && line.includes(entry.contains),
      );
      if (allowed >= 0) {
        used.add(allowed);
        continue;
      }
      failures.push(
        `${file}:${index + 1}: hand-written ${name} list; import the generated enums (contracts/gen/go/enums or contracts/gen/ts/dist/enums.js)`,
      );
    }
  });
}
ALLOWED.forEach((entry, index) => {
  if (!used.has(index))
    failures.push(`stale allowlist entry ${entry.file}: ${entry.contains}`);
});

// 3. Each module's own conformance test, against the generated values.
if (!copiesOnly) runModuleTests();

function runModuleTests() {
  run(
    "go",
    [
      "test",
      "./internal/app",
      "-run",
      "^(TestContractEnumConformance|TestInboxRespondAcceptsEveryContractOutcome)$",
    ],
    "cli",
  );
  run(
    "go",
    ["test", "./internal/server", "-run", "^TestContractEnumConformance$"],
    "core",
  );
  run(
    "go",
    [
      "test",
      "./internal/primitives",
      "-run",
      "^TestPrioritySQLMatchesContract$",
    ],
    "core",
  );
  run(
    "pnpm",
    ["exec", "vitest", "run", "tests/unit/contractEnums.test.js"],
    "web-ui",
  );
}

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log(
  copiesOnly
    ? "enum conformance: no hand-written copies of shared enums"
    : `enum conformance: ${schema.length} schema enums match core, CLI and web-ui`,
);
