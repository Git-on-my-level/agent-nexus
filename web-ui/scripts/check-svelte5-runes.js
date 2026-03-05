#!/usr/bin/env node
/**
 * Guard against Svelte 5 legacy reactivity in .svelte files.
 *
 * In Svelte 5, top-level `let` is not reactive; use $state() for reactive state.
 * The `$:` reactive statement is deprecated; use $derived() or $effect() instead.
 *
 * This script fails the build if it finds:
 * - Reactive statements: `$: ...`
 * - Top-level `let name = value` where value is not $state(...), $derived(...), or $props()
 *
 * Run as: node scripts/check-svelte5-runes.js [files...]
 * With no args, checks all .svelte files under src/
 */

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, isAbsolute } from "node:path";

const projectRoot = join(import.meta.dirname, "..");

function* walkSvelteFiles(dir, base) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    const stat = statSync(path);
    if (stat.isDirectory()) {
      if (name === "node_modules" || name === ".svelte-kit" || name === "build") continue;
      yield* walkSvelteFiles(path, base);
    } else if (name.endsWith(".svelte")) {
      yield relative(base, path);
    }
  }
}

function extractScriptContent(content) {
  const match = content.match(/<script(?:\s[^>]*)?>([\s\S]*?)<\/script>/);
  return match ? match[1] : "";
}

function getLines(scriptContent) {
  return scriptContent.split(/\r?\n/);
}

/**
 * Approximate "top level": not inside function/block body.
 * We track { } depth; top level is depth 0. Ignore depth inside strings and comments for simplicity.
 */
function isTopLevelLet(line) {
  return /^\s*let\s+\w+\s*=/.test(line) && !/^\s*let\s*\{/.test(line);
}

function lineHasRune(line) {
  return (
    line.includes("$state(") ||
    line.includes("$derived(") ||
    line.includes("$effect(") ||
    line.includes("$props()")
  );
}

/** Allow constants: UPPER_SNAKE_CASE name, or RHS is number/boolean/null (not string – often state). */
function looksLikeConstant(line) {
  const letMatch = line.match(/^\s*let\s+(\w+)\s*=\s*(.+)/);
  if (!letMatch) return false;
  const [, name, rhs] = letMatch;
  const trimmed = rhs.trim().replace(/;.*$/, "").trim();
  if (/^[A-Z][A-Z0-9_]*$/.test(name)) return true;
  if (/^(true|false|null|\d+)$/.test(trimmed)) return true;
  return false;
}

function checkFile(absPath) {
  const content = readFileSync(absPath, "utf8");
  const filePath = relative(projectRoot, absPath) || absPath;
  const scriptContent = extractScriptContent(content);
  if (!scriptContent.trim()) return []; // no script block

  const lines = getLines(scriptContent);
  const errors = [];
  let braceDepth = 0;
  let inBlockComment = false;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const lineNum = content.slice(0, content.indexOf(scriptContent)).split(/\r?\n/).length + i + 1;

    // Skip block comment lines
    if (line.includes("/*")) inBlockComment = true;
    if (inBlockComment) {
      if (line.includes("*/")) inBlockComment = false;
      continue;
    }
    if (inBlockComment) continue;

    // Reactive statement: $: ...
    if (/^\s*\$:/.test(line)) {
      errors.push({
        file: filePath,
        line: lineNum,
        message:
          "Use $derived() or $effect() instead of legacy reactive statement ($:). In Svelte 5, $: does not provide reliable reactivity.",
      });
    }

    // Track brace depth (simplified: count { and })
    const open = (line.match(/{/g) || []).length;
    const close = (line.match(/}/g) || []).length;
    braceDepth += open - close;

    // Top-level let without runes (exempt constants)
    if (
      braceDepth <= 0 &&
      isTopLevelLet(line) &&
      !lineHasRune(line) &&
      !looksLikeConstant(line)
    ) {
      errors.push({
        file: filePath,
        line: lineNum,
        message:
          "Reactive state must use $state(). In Svelte 5, top-level `let` is not reactive and UI will not update. Use: let x = $state(initialValue)",
      });
    }
  }

  return errors;
}

function main() {
  const files = process.argv.slice(2).length
    ? process.argv.slice(2)
    : [...walkSvelteFiles(join(projectRoot, "src"), projectRoot)];

  const allErrors = [];
  for (const f of files) {
    const absPath = isAbsolute(f) ? f : join(projectRoot, f);
    try {
      if (!absPath.endsWith(".svelte")) continue;
      allErrors.push(...checkFile(absPath));
    } catch (e) {
      if (e.code === "ENOENT") continue;
      throw e;
    }
  }

  if (allErrors.length === 0) {
    process.exit(0);
  }

  console.error("check-svelte5-runes: Svelte 5 runes guard found issues:\n");
  for (const { file, line, message } of allErrors) {
    console.error(`  ${file}:${line}: ${message}`);
  }
  console.error("\nSee: https://svelte.dev/docs/svelte/what-are-runes");
  process.exit(1);
}

main();
