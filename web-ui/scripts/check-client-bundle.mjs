#!/usr/bin/env node
/**
 * Fail if the browser client bundle still contains Node-only markdown stack markers
 * (e.g. jsdom leaked into the client graph). Run after `vite build`.
 */
import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";

const clientDir = join(".svelte-kit", "output", "client");

async function walk(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  const files = [];
  for (const e of entries) {
    const p = join(dir, e.name);
    if (e.isDirectory()) {
      files.push(...(await walk(p)));
    } else if (
      e.isFile() &&
      (e.name.endsWith(".js") || e.name.endsWith(".mjs"))
    ) {
      files.push(p);
    }
  }
  return files;
}

async function main() {
  let files;
  try {
    files = await walk(clientDir);
  } catch {
    console.error(
      `check-client-bundle: missing ${clientDir}; run vite build first.`,
    );
    process.exit(1);
  }

  const needles = ["jsdom", "webidl-conversions"];
  const hits = [];
  for (const f of files) {
    const content = await readFile(f, "utf8");
    for (const n of needles) {
      if (content.includes(n)) {
        hits.push({ file: f, needle: n });
      }
    }
  }

  if (hits.length > 0) {
    console.error(
      "check-client-bundle: forbidden substrings in client JS (Node-only deps may have been bundled):",
    );
    for (const h of hits) {
      console.error(`  ${h.needle} -> ${h.file}`);
    }
    process.exit(1);
  }

  console.log(
    `check-client-bundle: ok (${files.length} client JS file${files.length === 1 ? "" : "s"}).`,
  );
}

await main();
