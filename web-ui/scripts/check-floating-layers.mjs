#!/usr/bin/env node
/**
 * Static guard for a rendering bug class: a viewport-fixed layer that is not
 * a full-screen scrim but has a translucent fill. Whatever scrolls beneath it
 * stays readable through it (text over text) and its controls get covered.
 *
 * Flags `class="... fixed ..."` attributes in Svelte files whose only
 * background is translucent (a `-soft` token, a slash-opacity color such as
 * `bg-black/60`, or `bg-transparent`).
 * Fix by rendering the content in flow, using a modal with an opaque panel,
 * or giving the layer an opaque surface (`bg-panel`, `bg-bg`, `bg-bg-soft`).
 * Opt out for a deliberate case with `data-floating-layer-ok` on the element.
 *
 * The runtime counterpart is tests/helpers/layoutAudit.js.
 */
import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function* svelteFiles(dir) {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      yield* svelteFiles(full);
    } else if (entry.endsWith(".svelte")) {
      yield full;
    }
  }
}

// `bg-bg-soft` is an opaque surface token despite the name.
const OPAQUE_SOFT = new Set(["bg-bg-soft"]);

function isTranslucentBg(token) {
  const base = token.split(":").pop();
  if (!base.startsWith("bg-")) return false;
  if (OPAQUE_SOFT.has(base)) return false;
  return (
    base === "bg-transparent" || base.endsWith("-soft") || /\/\d+$/.test(base)
  );
}

function isBg(token) {
  const base = token.split(":").pop();
  return (
    base.startsWith("bg-") &&
    !/^bg-(gradient|clip|no-repeat|cover|center|opacity)/.test(base)
  );
}

const failures = [];
for (const file of svelteFiles(path.join(root, "src"))) {
  const source = readFileSync(file, "utf8");
  const tagPattern = /<[a-zA-Z][^<>]*?\bclass="([^"]*)"[^<>]*?>/gs;
  for (const match of source.matchAll(tagPattern)) {
    const tokens = match[1]
      .replace(/\{[^}]*\}/g, " ")
      .split(/\s+/)
      .filter(Boolean);
    if (!tokens.includes("fixed")) continue;
    if (tokens.includes("inset-0")) continue; // full-screen scrim
    if (match[0].includes("data-floating-layer-ok")) continue;
    const backgrounds = tokens.filter(isBg);
    if (backgrounds.length === 0) continue; // layout-only wrapper
    if (!backgrounds.every(isTranslucentBg)) continue;
    const line = source.slice(0, match.index).split("\n").length;
    failures.push(
      `${path.relative(root, file)}:${line}  fixed layer with translucent background (${backgrounds.join(", ")})`,
    );
  }
}

if (failures.length > 0) {
  console.error(
    "Floating layers need an opaque surface (see scripts/check-floating-layers.mjs):",
  );
  for (const failure of failures) console.error(`  ${failure}`);
  process.exit(1);
}
