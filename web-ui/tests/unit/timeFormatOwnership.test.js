import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

/**
 * One formatter for a timestamp, enforced.
 *
 * Display text goes through `src/lib/time`. Any `toLocaleString`,
 * `toLocaleDateString`, `toLocaleTimeString`, `Intl.DateTimeFormat`,
 * `Intl.RelativeTimeFormat`, or a hand-built "ago" / "min ago" phrase
 * outside that module is how the dashboard ended up showing
 * `2026-10-05 05:56 UTC`. Wire timestamps (`toISOString` sent to the API)
 * and `Intl.NumberFormat` counts are not display and are not flagged.
 *
 * A file that formats a date for a reason other than showing an instant
 * starts with `time-guard: not-display: <reason>`.
 */

const root = fileURLToPath(new URL("../../src", import.meta.url));

const DISPLAY = [
  /toLocale(?:Date|Time)?String\s*\(/,
  /Intl\.DateTimeFormat/,
  /Intl\.RelativeTimeFormat/,
  /\$\{[^}\n]*\}\s*(?:[A-Za-z]+\s+)?ago\b/,
  /\{[^{}#/\n][^}\n]*\}\s*ago\b/,
  /`[^`\n]*\bmin ago\b/,
  /["'][^"'\n]*\bmin ago\b/,
  /\+\s*["'`][^"'`\n]*\bago\b/,
  /hour\s*:\s*["'](?:numeric|2-digit)/,
  /\.getHours\s*\(/,
  /toISOString\(\)\s*\.(?:slice|replace|split|substring)/,
  /(?:"|'|`) UTC/,
  /\+\s*(?:"|'|`) UTC/,
  /timeZoneName\s*:/,
  /hour12\s*:/,
];

function codeOnly(source) {
  return source
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^[ \t]*\/\/[^\n]*/gm, "");
}

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
  const code = codeOnly(source);
  return DISPLAY.filter((pattern) => pattern.test(code)).map(
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

  it("catches each way to format a timestamp by hand", () => {
    const samples = [
      'new Date(v).toLocaleString("en-US")',
      "new Date(v).toLocaleDateString()",
      "new Date(v).toLocaleTimeString()",
      'new Intl.DateTimeFormat("en-US").format(d)',
      'Intl.RelativeTimeFormat("en").format(-1, "day")',
      "`${minutes} min ago`",
      "`${hours} h ago`",
      "{askedAgo} ago",
      '"15 min ago"',
      'minutes + " min ago"',
    ];
    for (const sample of samples) {
      expect(hits(sample), sample).not.toEqual([]);
    }
    expect(hits("value.toISOString()")).toEqual([]);
    expect(hits('new Intl.NumberFormat("en-US").format(n)')).toEqual([]);
    expect(
      hits("/* ${minutes} min ago */\nconst wire = value.toISOString();"),
    ).toEqual([]);
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
