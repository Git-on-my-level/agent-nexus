import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import {
  DEFAULT_PM_RUNNER_KEY,
  PM_RUNNERS,
  pmRunnerFor,
} from "../../src/lib/setup/pmRunners.js";

/**
 * The PM runner picker produces a `--runner` the CLI has to accept without a
 * human answering the wizard. The argv strings therefore have to be the same
 * ones `anx pm install`'s wizard offers, and they live in two languages.
 *
 * This reads the wizard source and fails when either side moves, which is the
 * whole reason the duplication is allowed to exist.
 */
const WIZARD = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../cli/internal/app/pm_wizard.go",
);

describe("PM runners match the CLI wizard", () => {
  const source = readFileSync(WIZARD, "utf8");

  it("offers every runner the wizard does, and no others", () => {
    expect(PM_RUNNERS.map((runner) => runner.key)).toEqual([
      "hermes",
      "claude",
    ]);
    // The wizard's menu, which is what a reader would otherwise have to answer.
    expect(source).toContain(
      `"1. Hermes\\n2. Claude Code\\n3. Custom command"`,
    );
  });

  it.each(PM_RUNNERS.map((runner) => [runner.key, runner.argv]))(
    "pins the %s runner argv",
    (_key, argv) => {
      // Go source quotes these either as an interpreted or a raw string
      // literal; the bytes between the quotes are what has to match.
      expect(
        source.includes(`"${argv}"`) || source.includes(`\`${argv}\``),
      ).toBe(true);
    },
  );

  it("defaults to a runner that exists", () => {
    expect(
      PM_RUNNERS.some((runner) => runner.key === DEFAULT_PM_RUNNER_KEY),
    ).toBe(true);
    expect(pmRunnerFor("").key).toBe(DEFAULT_PM_RUNNER_KEY);
    expect(pmRunnerFor("nonsense").key).toBe(DEFAULT_PM_RUNNER_KEY);
    expect(pmRunnerFor("hermes").key).toBe("hermes");
  });

  it("keeps {prompt_file} in every runner the CLI substitutes into", () => {
    for (const runner of PM_RUNNERS) {
      expect(runner.argv).toContain("{prompt_file}");
    }
  });
});
