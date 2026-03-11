import { commandRegistry } from "../../../contracts/gen/ts/dist/client.js";

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
  const cryptoImpl = globalThis.crypto?.subtle;
  if (!cryptoImpl) {
    throw new Error("Web Crypto is unavailable for command registry checks.");
  }

  const input = new TextEncoder().encode(value);
  const digest = await cryptoImpl.digest("SHA-256", input);
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
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
