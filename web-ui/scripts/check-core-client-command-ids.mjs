#!/usr/bin/env node
/**
 * Guardrail: every command id wired in anxCoreClient.js must exist in the
 * generated contracts command registry (same source as the generated TS client).
 *
 * Run: node scripts/check-core-client-command-ids.mjs
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { assertCoreClientCommandRegistry } from "../src/lib/coreClientCommandRegistry.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const anxCoreClientPath = path.join(__dirname, "../src/lib/anxCoreClient.js");

try {
  const source = fs.readFileSync(anxCoreClientPath, "utf8");
  const commandIds = assertCoreClientCommandRegistry({ source });
  console.error(
    `check-core-client-command-ids: ok (${commandIds.length} bindings).`,
  );
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  console.error(`check-core-client-command-ids: ${message}`);
  process.exit(1);
}
