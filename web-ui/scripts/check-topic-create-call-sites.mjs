#!/usr/bin/env node
/**
 * Guardrail: UI code should call coreClient.createTopic only with shared builders
 * from topicCreatePayload.js (buildTopicCreatePayloadFromDraft)
 * so POST /topics bodies stay aligned with core validation.
 *
 * Allowed:
 *   coreClient.createTopic(buildTopicCreatePayloadFromDraft(...))
 *   Multiline calls where the first argument expression starts with buildTopicCreatePayload
 *
 * Excludes: src/lib/anxCoreClient.js (API definition only).
 *
 * Run: node scripts/check-topic-create-call-sites.mjs
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcRoot = path.join(__dirname, "..", "src");
const needle = ".createTopic(";

function walk(dir, acc = []) {
  for (const ent of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, ent.name);
    if (ent.isDirectory()) {
      walk(p, acc);
    } else if (/\.(js|svelte)$/.test(ent.name)) {
      acc.push(p);
    }
  }
  return acc;
}

/**
 * Parse a parenthesized call starting at openParenIndex (the `(`).
 * Returns inner slice (all arguments joined) and index after the closing `)`.
 */
function parseCallAfterOpenParen(text, openParenIndex) {
  let depth = 0;
  let i = openParenIndex;
  if (text[i] !== "(") {
    return null;
  }
  depth = 1;
  i++;
  const start = i;
  let inString = null;
  let escape = false;
  for (; i < text.length; i++) {
    const c = text[i];
    if (inString) {
      if (escape) {
        escape = false;
        continue;
      }
      if (c === "\\") {
        escape = true;
        continue;
      }
      if (c === inString) {
        inString = null;
        continue;
      }
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      inString = c;
      continue;
    }
    if (c === "(") {
      depth++;
    } else if (c === ")") {
      depth--;
      if (depth === 0) {
        return { inner: text.slice(start, i).trim(), nextIndex: i + 1 };
      }
    }
  }
  return null;
}

function checkFile(filePath) {
  const rel = path.relative(path.join(__dirname, ".."), filePath);
  if (rel === path.join("src", "lib", "anxCoreClient.js")) {
    return [];
  }

  const text = fs.readFileSync(filePath, "utf8");
  const issues = [];
  let from = 0;
  while (true) {
    const i = text.indexOf(needle, from);
    if (i === -1) {
      break;
    }
    const openParen = i + needle.length - 1;
    const parsed = parseCallAfterOpenParen(text, openParen);
    if (parsed === null) {
      const line = text.slice(0, i).split("\n").length;
      issues.push({ file: rel, line, reason: "unbalanced call" });
      from = i + needle.length;
      continue;
    }
    if (!parsed.inner.startsWith("buildTopicCreatePayload")) {
      const line = text.slice(0, i).split("\n").length;
      issues.push({
        file: rel,
        line,
        reason: `expected buildTopicCreatePayload*, got: ${parsed.inner.slice(0, 72)}${parsed.inner.length > 72 ? "…" : ""}`,
      });
    }
    from = parsed.nextIndex;
  }
  return issues;
}

const files = walk(srcRoot);
const all = [];
for (const f of files) {
  all.push(...checkFile(f));
}

if (all.length > 0) {
  console.error(
    "check-topic-create-call-sites: use buildTopicCreatePayload* for createTopic:\n",
  );
  for (const e of all) {
    console.error(`  ${e.file}:${e.line} — ${e.reason}`);
  }
  process.exit(1);
}

console.error("check-topic-create-call-sites: ok.");
