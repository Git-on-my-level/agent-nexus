#!/usr/bin/env node
/**
 * Flags same-origin core client paths that would NOT be proxied to oar-core
 * (would hit SvelteKit and often return HTML 404), excluding known exceptions.
 *
 * Run: node scripts/audit-core-proxy-paths.mjs
 */
import { isProxyableCommand } from "../src/lib/coreRouteCatalog.js";
import {
  isDirectCoreProxyPath,
  isWebUiOwnedAuthPath,
} from "../src/lib/server/directCoreProxyPaths.js";

/** @typedef {{ method: string, path: string, note?: string }} Sample */

/** Representative paths from the browser client (invokeJSON + invokeDirectRaw). */
const samples = /** @type {Sample[]} */ ([
  { method: "GET", path: "/meta/handshake" },
  { method: "GET", path: "/actors" },
  { method: "POST", path: "/actors" },
  { method: "POST", path: "/auth/token" },
  { method: "GET", path: "/agents/me", note: "getCurrentAgent" },
  { method: "POST", path: "/auth/passkey/register/options" },
  { method: "POST", path: "/auth/passkey/register/verify" },
  { method: "POST", path: "/auth/passkey/login/options" },
  { method: "POST", path: "/auth/passkey/login/verify" },
  { method: "GET", path: "/auth/bootstrap/status" },
  { method: "GET", path: "/auth/invites" },
  { method: "POST", path: "/auth/invites" },
  { method: "GET", path: "/auth/principals" },
  { method: "GET", path: "/auth/audit" },
  { method: "GET", path: "/events/stream" },
  { method: "POST", path: "/topics/t-1/archive" },
  { method: "POST", path: "/topics/t-1/unarchive" },
  { method: "POST", path: "/topics/t-1/trash" },
  { method: "POST", path: "/topics/t-1/restore" },
  { method: "POST", path: "/artifacts" },
  { method: "GET", path: "/artifacts" },
  { method: "POST", path: "/artifacts/a-1/archive" },
  { method: "GET", path: "/artifacts/a-1/content", note: "getArtifactContent" },
  { method: "POST", path: "/docs/d-1/revisions", note: "updateDocument" },
  { method: "POST", path: "/docs/d-1/trash" },
  { method: "GET", path: "/events/e-1", note: "getEvent" },
  { method: "POST", path: "/events/e-1/trash" },
  { method: "PATCH", path: "/boards/b-1", note: "updateBoard" },
  { method: "PATCH", path: "/cards/c-1", note: "updateBoardCard" },
]);

function classify(method, path) {
  if (isWebUiOwnedAuthPath(path)) {
    return "web_ui_auth";
  }
  if (isProxyableCommand(method, path)) {
    return "catalog";
  }
  if (isDirectCoreProxyPath(method, path)) {
    return "hook_only";
  }
  return "unproxied";
}

const unproxied = [];

for (const { method, path, note } of samples) {
  const bucket = classify(method, path);
  if (bucket === "unproxied") {
    unproxied.push({ method, path, note });
  }
}

console.log("core client path audit (same-origin /api proxy rules)\n");

if (unproxied.length === 0) {
  console.log("No unproxied invokeDirect paths in this sample set.\n");
  process.exit(0);
}

console.log(
  "These paths are NOT matched by isProxyableCommand ∪ isDirectCoreProxyPath",
);
console.log(
  "(browser fetch to the UI origin would not be forwarded to oar-core):\n",
);
for (const row of unproxied) {
  const extra = row.note ? `  (${row.note})` : "";
  console.log(`  ${row.method} ${row.path}${extra}`);
}
console.log(
  "\nNote: paths under isWebUiOwnedAuthPath are handled by SvelteKit by design.\n",
);
process.exit(unproxied.length > 0 ? 1 : 0);
