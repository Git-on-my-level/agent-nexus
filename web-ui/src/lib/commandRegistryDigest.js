import { commandRegistry } from "../../../contracts/gen/ts/dist/client.js";
import { sha256Hex as sha256HexBytes } from "./cryptoFallback.js";

function canonicalCommandRegistryEntries(commands = commandRegistry) {
  return [...commands]
    .map((command) => {
      const commandId = String(command?.command_id ?? "").trim();
      const method = String(command?.method ?? "")
        .trim()
        .toUpperCase();
      const path = String(command?.path ?? "").trim();
      if (!commandId || !method || !path) {
        return "";
      }
      return `${commandId}|${method}|${path}`;
    })
    .filter(Boolean)
    .sort();
}

async function sha256Hex(value) {
  const input = new TextEncoder().encode(value);
  return sha256HexBytes(input);
}

let expectedCommandRegistryDigestPromise;

export function getExpectedCommandRegistryDigest() {
  if (!expectedCommandRegistryDigestPromise) {
    expectedCommandRegistryDigestPromise = sha256Hex(
      canonicalCommandRegistryEntries().join("\n"),
    );
  }

  return expectedCommandRegistryDigestPromise;
}
