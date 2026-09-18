import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { HOME_FEED_PRESET } from "../../src/lib/events/eventRows.js";
import { compactNounForPrefix } from "../../src/lib/refLinkModel.js";

const srcRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../src");

function readSrc(relativePath) {
  return readFileSync(resolve(srcRoot, relativePath), "utf8");
}

/**
 * anx-ui-spec.md §1.8 bans Card/Cards as operator labels. These tests pull the
 * copy out of the surfaces that teach an operator the vocabulary and assert the
 * banned noun is absent, rather than pinning today's exact sentences — a test
 * that only knows the current wording cannot catch the next violation, which is
 * how "Drag a card between phases" survived the rename in the first place.
 */
const BANNED_OPERATOR_NOUN = /\bcards?\b/i;

/** String literals assigned to the given keys, e.g. `body: "…"`. */
function literalsForKeys(source, keys) {
  const pattern = new RegExp(
    `\\b(?:${keys.join("|")}):\\s*"((?:[^"\\\\]|\\\\.)*)"`,
    "g",
  );
  return [...source.matchAll(pattern)].map((match) => match[1]);
}

describe("operator vocabulary", () => {
  it("keeps the Watching projection on core's stored preset id", () => {
    // Watching is the operator label; `home_feed` is core's stored id, backing
    // /home/read and home_read_cursors. Renaming the id is a storage migration.
    expect(HOME_FEED_PRESET).toBe("home_feed");
  });

  it("labels the Audit preset Watching, not Home feed", () => {
    const src = readSrc(
      "routes/o/[organization]/w/[workspace]/events/+page.svelte",
    );
    expect(src).toContain(`<option value={HOME_FEED_PRESET}>Watching</option>`);
    expect(src).not.toMatch(/>\s*Home feed\s*</);
  });

  it("uses operator nouns on compact RefLink chips", () => {
    expect(compactNounForPrefix("card")).toBe("Task");
    expect(compactNounForPrefix("topic")).toBe("Project");
    // RefLink must not reintroduce its own map: that drift is the bug.
    expect(readSrc("lib/components/RefLink.svelte")).not.toMatch(
      /nounByPrefix\s*=/,
    );
  });

  it("never says card in onboarding tour copy", () => {
    const steps = literalsForKeys(
      readSrc("lib/components/onboarding/WorkspaceTour.svelte"),
      ["title", "eyebrow", "body"],
    );
    expect(steps.length).toBeGreaterThan(0);
    for (const step of steps) {
      expect(step, `tour copy: ${step}`).not.toMatch(BANNED_OPERATOR_NOUN);
    }
  });

  it("never says card in the Tasks board drag instructions", () => {
    const src = readSrc("lib/components/pm/WorkViews.svelte");
    const help = src.match(
      /<p id="task-board-card-help"[^>]*>([\s\S]*?)<\/p>/,
    )?.[1];
    expect(help, "task-board-card-help paragraph not found").toBeTruthy();
    expect(help).not.toMatch(BANNED_OPERATOR_NOUN);
  });

  it("never says card in the Tasks keyboard shortcut list", () => {
    const src = readSrc(
      "routes/o/[organization]/w/[workspace]/tasks/+page.svelte",
    );
    const list = src.match(/SHORTCUTS\s*=\s*\[([\s\S]*?)\n\s*\];/)?.[1];
    expect(list, "shortcut list not found").toBeTruthy();
    const labels = [...list.matchAll(/\[\s*"((?:[^"\\]|\\.)*)"/g)].map(
      (match) => match[1],
    );
    expect(labels.length).toBeGreaterThan(0);
    for (const label of labels) {
      expect(label, `shortcut label: ${label}`).not.toMatch(
        BANNED_OPERATOR_NOUN,
      );
    }
  });
});
