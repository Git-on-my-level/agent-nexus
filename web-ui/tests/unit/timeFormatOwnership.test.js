import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

/**
 * One formatter for a timestamp, enforced.
 *
 * Display text goes through `src/lib/time`. A second `toLocaleDateString`,
 * a sliced ISO string, or a hand-built "UTC" clock is how the dashboard
 * ended up showing `2026-10-05 05:56 UTC`. Wire timestamps (`toISOString`
 * sent to the API) are not display and are not flagged.
 *
 * A file that formats a date for a reason other than showing an instant
 * starts with `time-guard: not-display: <reason>`.
 */

const root = fileURLToPath(new URL("../../src", import.meta.url));

const DISPLAY = [
  /toLocaleDateString\s*\(/,
  /toLocaleTimeString\s*\(/,
  /toLocaleString\s*\(\s*\)/,
  /toLocaleString\s*\(\s*(?:undefined|locale)\b/,
  /hour\s*:\s*["'](?:numeric|2-digit)/,
  /\.getHours\s*\(/,
  /new\s+Intl\.DateTimeFormat/,
  /Intl\.RelativeTimeFormat/,
  /toISOString\(\)\s*\.(?:slice|replace|split|substring)/,
  /(?:"|'|`) UTC/,
  /\+\s*(?:"|'|`) UTC/,
  /timeZoneName\s*:/,
  /hour12\s*:/,
];

function sourceFiles(dir = root, prefix = "src") {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true }).sort((a, b) =>
    a.name.localeCompare(b.name),
  )) {
    const name = `${prefix}/${entry.name}`;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "generated") continue;
      out.push(...sourceFiles(full, name));
    } else if (/\.(svelte|js)$/.test(entry.name)) {
      out.push(name);
    }
  }
  return out;
}

const rawSource = (file) => readFileSync(join(root, "..", file), "utf8");

function exemption(file, source) {
  const header = source.match(
    /^\s*(?:\/\/([^\n]*)|\/\*([\s\S]*?)\*\/|<!--([\s\S]*?)-->)/,
  );
  const comment = header
    ?.slice(1)
    .find((part) => part !== undefined)
    ?.trim();
  if (!comment?.startsWith("time-guard:")) return false;
  if (!/^time-guard: not-display: [a-zA-Z].+/.test(comment)) {
    throw new Error(`${file}: time-guard requires a reason`);
  }
  return true;
}

function hits(source) {
  return DISPLAY.filter((pattern) => pattern.test(source)).map(
    (pattern) => pattern.source,
  );
}

const sources = sourceFiles();

describe("timestamps are formatted in one module", () => {
  it("keeps display formatting inside src/lib/time", () => {
    const offenders = [];
    for (const file of sources) {
      if (file.startsWith("src/lib/time/")) continue;
      const source = rawSource(file);
      if (exemption(file, source)) continue;
      const found = hits(source);
      if (found.length) offenders.push(`${file}: ${found.join(", ")}`);
    }
    expect(offenders).toEqual([]);
  });

  it("requires a reason on an exemption", () => {
    expect(() => exemption("a.js", "// time-guard: not-display")).toThrow(
      "requires a reason",
    );
    expect(
      exemption(
        "a.js",
        "// time-guard: not-display: Cron fields are a wall clock, not an instant.",
      ),
    ).toBe(true);
    expect(exemption("a.js", 'const x = "time-guard: not-display: no";')).toBe(
      false,
    );
  });
});
