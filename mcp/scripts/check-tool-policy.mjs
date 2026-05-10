#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const repoRoot = path.resolve(import.meta.dirname, "../..");
const commandsPath = path.join(repoRoot, "contracts/gen/meta/commands.json");
const policyPath = path.join(repoRoot, "mcp/policy/default_tool_policy.yaml");
const reportPath = path.join(repoRoot, "mcp/docs/tool-coverage.md");

const validClassifications = new Set([
  "exposed_read",
  "exposed_write",
  "gated_admin",
  "gated_sensitive",
  "adapted",
  "unsupported_interactive",
  "unsupported_streaming",
  "unsupported_bootstrap_auth",
  "unsupported_shell_shaped",
  "unsupported_other",
]);

function unquote(value) {
  const trimmed = value.trim();
  if (
    (trimmed.startsWith('"') && trimmed.endsWith('"')) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'"))
  ) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function parsePolicy(text) {
  const commands = new Map();
  let inCommands = false;
  let currentID = "";

  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.replace(/\s+$/, "");
    if (!line || line.trimStart().startsWith("#")) {
      continue;
    }
    if (line === "commands:") {
      inCommands = true;
      continue;
    }
    if (!inCommands) {
      continue;
    }

    const commandMatch = line.match(/^  "([^"]+)":\s*$/);
    if (commandMatch) {
      currentID = commandMatch[1];
      if (commands.has(currentID)) {
        throw new Error(`duplicate policy command ${currentID}`);
      }
      commands.set(currentID, {});
      continue;
    }

    const fieldMatch = line.match(/^    ([a-z_]+):\s*(.+)$/);
    if (fieldMatch && currentID) {
      commands.get(currentID)[fieldMatch[1]] = unquote(fieldMatch[2]);
      continue;
    }

    throw new Error(`unsupported policy YAML line: ${rawLine}`);
  }

  return commands;
}

function commandGroup(command) {
  return command.group || command.command_id.split(".")[0];
}

function countBy(items, keyFn) {
  const counts = new Map();
  for (const item of items) {
    const key = keyFn(item);
    counts.set(key, (counts.get(key) || 0) + 1);
  }
  return [...counts.entries()].sort(([a], [b]) => a.localeCompare(b));
}

function markdownTable(headers, rows) {
  return [
    `| ${headers.join(" | ")} |`,
    `| ${headers.map(() => "---").join(" | ")} |`,
    ...rows.map((row) => `| ${row.join(" | ")} |`),
  ].join("\n");
}

function buildReport(metadata, policy) {
  const commands = metadata.commands.map((command) => ({
    ...command,
    group: commandGroup(command),
    policy: policy.get(command.command_id),
  }));

  const groupRows = countBy(commands, (command) => command.group).map(([group, count]) => [
    group,
    String(count),
  ]);
  const classificationRows = countBy(commands, (command) => command.policy.classification).map(
    ([classification, count]) => [classification, String(count)],
  );
  const commandRows = commands
    .slice()
    .sort((a, b) => a.command_id.localeCompare(b.command_id))
    .map((command) => [
      command.command_id,
      command.group,
      command.method || "",
      command.path || "",
      command.policy.classification,
      command.policy.reason,
    ]);

  return `# MCP Tool Coverage

Generated from \`contracts/gen/meta/commands.json\` and \`mcp/policy/default_tool_policy.yaml\`.

- Command count: ${metadata.command_count}
- Contract version: ${metadata.contract_version}
- OpenAPI version: ${metadata.openapi_version}

## Counts by Group

${markdownTable(["Group", "Commands"], groupRows)}

## Counts by Classification

${markdownTable(["Classification", "Commands"], classificationRows)}

## Command Inventory

${markdownTable(["Command", "Group", "Method", "Path", "Classification", "Reason"], commandRows)}
`;
}

function main() {
  const writeReport = process.argv.includes("--write-report");
  const metadata = JSON.parse(fs.readFileSync(commandsPath, "utf8"));
  const policy = parsePolicy(fs.readFileSync(policyPath, "utf8"));
  const errors = [];

  if (!Array.isArray(metadata.commands)) {
    errors.push("commands metadata does not contain a commands array");
  }
  if (metadata.command_count !== metadata.commands.length) {
    errors.push(
      `metadata command_count=${metadata.command_count} but commands length=${metadata.commands.length}`,
    );
  }

  const metadataIDs = new Set(metadata.commands.map((command) => command.command_id));
  for (const command of metadata.commands) {
    const entry = policy.get(command.command_id);
    if (!entry) {
      errors.push(`missing policy entry for ${command.command_id}`);
      continue;
    }
    if (!validClassifications.has(entry.classification)) {
      errors.push(`${command.command_id} has invalid classification ${entry.classification || ""}`);
    }
    if (!entry.reason || entry.reason.length < 8) {
      errors.push(`${command.command_id} needs a stable reason`);
    }
  }
  for (const policyID of policy.keys()) {
    if (!metadataIDs.has(policyID)) {
      errors.push(`policy entry ${policyID} is not in generated command metadata`);
    }
  }

  if (errors.length > 0) {
    for (const error of errors) {
      console.error(`tool policy error: ${error}`);
    }
    process.exit(1);
  }

  const report = buildReport(metadata, policy);
  if (writeReport) {
    fs.writeFileSync(reportPath, report);
  } else if (fs.existsSync(reportPath) && fs.readFileSync(reportPath, "utf8") !== report) {
    console.error("tool policy error: mcp/docs/tool-coverage.md is stale; run with --write-report");
    process.exit(1);
  }

  const classifications = countBy(metadata.commands, (command) =>
    policy.get(command.command_id).classification,
  )
    .map(([classification, count]) => `${classification}=${count}`)
    .join(" ");
  console.log(
    `tool policy ok: ${metadata.commands.length} commands, ${policy.size} policy entries; ${classifications}`,
  );
}

main();
