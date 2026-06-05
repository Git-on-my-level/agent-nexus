import { commandRegistry } from "../../../contracts/gen/ts/dist/client.js";

const ADAPTER_ENTRY_COMMAND_ID =
  /\[\s*(?:\n\s*)?"[^"]+"\s*,\s*(?:\n\s*)?"([a-z][a-z0-9_.]+)"/g;
const INVOKE_COMMAND_ID = /invokeCommand\(\s*"([a-z][a-z0-9_.]+)"/g;

/**
 * Collect command ids wired in anxCoreClient.js (adapter table + invokeCommand calls).
 *
 * @param {string} source
 * @returns {string[]}
 */
export function extractCoreClientCommandIdsFromSource(source) {
  const tableStart = source.indexOf("const adapterCommandTable = [");
  if (tableStart < 0) {
    throw new Error(
      "anxCoreClient.js is missing const adapterCommandTable = [",
    );
  }

  const tableEnd = source.indexOf("\n];", tableStart);
  if (tableEnd < 0) {
    throw new Error("anxCoreClient.js adapterCommandTable is not closed");
  }

  const tableBlock = source.slice(tableStart, tableEnd);
  const ids = new Set();

  for (const match of tableBlock.matchAll(ADAPTER_ENTRY_COMMAND_ID)) {
    ids.add(match[1]);
  }

  for (const match of source.matchAll(INVOKE_COMMAND_ID)) {
    ids.add(match[1]);
  }

  return [...ids].sort();
}

/**
 * @param {string[]} commandIds
 * @param {typeof commandRegistry} [commands]
 * @returns {{ commandId: string, reason: string }[]}
 */
export function findCoreClientCommandRegistryIssues({
  commandIds,
  commands = commandRegistry,
} = {}) {
  if (!Array.isArray(commandIds) || commandIds.length === 0) {
    throw new Error("commandIds is required");
  }

  const byId = new Map(
    commands.map((command) => [command.command_id, command]),
  );
  const issues = [];

  for (const commandId of commandIds) {
    const command = byId.get(commandId);
    if (!command) {
      issues.push({
        commandId,
        reason: "missing from generated command registry",
      });
      continue;
    }

    if (!String(command.ts_method ?? "").trim()) {
      issues.push({
        commandId,
        reason: "registry entry is missing ts_method",
      });
    }
  }

  return issues;
}

/**
 * @param {{ source?: string, commandIds?: string[], commands?: typeof commandRegistry }} [opts]
 */
export function assertCoreClientCommandRegistry({
  source,
  commandIds,
  commands = commandRegistry,
} = {}) {
  const resolvedCommandIds =
    commandIds ??
    (source
      ? extractCoreClientCommandIdsFromSource(source)
      : (() => {
          throw new Error("source or commandIds is required");
        })());

  const issues = findCoreClientCommandRegistryIssues({
    commandIds: resolvedCommandIds,
    commands,
  });

  if (issues.length === 0) {
    return resolvedCommandIds;
  }

  const detail = issues
    .map((issue) => `${issue.commandId} (${issue.reason})`)
    .join(", ");

  throw new Error(
    `web-ui core client references invalid command ids: ${detail}. ` +
      "Update anxCoreClient bindings to match contracts/gen commandRegistry, or add the command to the contract and run make contract-gen.",
  );
}
