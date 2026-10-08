import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { searchWork } from "../../src/lib/searchHelpers.js";
import { loadOverview } from "../../src/lib/overview.js";
import { workProse } from "../../src/lib/workSummary.js";

/**
 * Core computes the shared summary only when asked.
 *
 * `work_summary` was required on every card read in the first draft of the
 * contract and became an opt-in before it merged: without `summary=1` core
 * answers with the legacy prose `summary` and no computed parts. A client
 * that forgets the parameter still renders — the back-compatibility reader
 * sees to that — so forgetting it is invisible, and the surfaces quietly go
 * back to disagreeing. These tests are what makes it visible.
 *
 * `summary=1` also moves the prose: the object takes `summary` and the body
 * moves to `summary_text`. Anything rendering a card's body reads both.
 */

describe("every card read asks for the computed summary", () => {
  it("overview", async () => {
    const asked = [];
    await loadOverview({
      getOverview: async (filters) => {
        asked.push(filters);
        return {
          work: { status: "ok", items: [] },
          initiatives: { status: "ok", items: [] },
          needs_you: { status: "ok", count: 0, rows: [], href: "" },
          dashboard: { status: "ok", reports: [] },
          agents: { status: "ok", items: [] },
        };
      },
    });
    expect(asked[0]).toMatchObject({ summary: 1 });
  });

  it("work search, which the command palette shows status in", async () => {
    const asked = [];
    const client = await import("../../src/lib/coreClient.js").catch(
      () => null,
    );
    void client;
    // `searchWork` closes over the shared client, so the parameter is read
    // from the source rather than from a stub: it is a one-line call.
    const source = readFileSync(
      fileURLToPath(new URL("../../src/lib/searchHelpers.js", import.meta.url)),
      "utf8",
    );
    const call = source.slice(
      source.indexOf("export async function searchWork"),
    );
    expect(call.slice(0, call.indexOf("}"))).toContain("summary: 1");
    expect(typeof searchWork).toBe("function");
    void asked;
  });
});

describe("no card read is left opted out", () => {
  /*
   * A mechanical sweep rather than a list: a new `listWork` or `getWork` call
   * that forgets the parameter renders a status from the legacy fields and
   * nothing fails, which is the failure mode this whole change exists to end.
   */
  const root = fileURLToPath(new URL("../../src", import.meta.url));
  /**
   * Every source file under `src`, as repo-relative paths.
   *
   * A plain walk rather than `globSync` with `withFileTypes`: a Dirent's
   * `parentPath` is absolute on one Node and relative to `cwd` on another,
   * and the arithmetic that turned it into a name produced different file
   * sets here and in CI.
   */
  function sourceFiles(dir = root, prefix = "src") {
    const out = [];
    for (const entry of readdirSync(dir, { withFileTypes: true }).sort((a, b) =>
      a.name.localeCompare(b.name),
    )) {
      const name = `${prefix}/${entry.name}`;
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === "generated" || entry.name === "dev") continue;
        out.push(...sourceFiles(full, name));
      } else if (
        /\.(svelte|js)$/.test(entry.name) &&
        name !== "src/lib/anxCoreClient.js"
      ) {
        out.push(name);
      }
    }
    return out;
  }
  const sources = sourceFiles();

  /**
   * Reads that show no card state, and so should not pay for one.
   *
   * Keyed on the method as well as the file: keyed on the file alone,
   * excusing the task page's mirrors `listWork` also excused the card read
   * the whole page is built on.
   */
  const NO_STATE_SHOWN = new Map([
    [
      "listWork src/lib/inboxSources.js",
      "names the source behind an inbox row",
    ],
    [
      "listWork src/routes/o/[organization]/w/[workspace]/integrations/+page.svelte",
      "counts and lists tasks per connection; shows read freshness, not state",
    ],
    [
      "listWork src/routes/o/[organization]/w/[workspace]/pm/+page.svelte",
      "picks a task by title for a PM conversation",
    ],
    [
      "listWork src/routes/o/[organization]/w/[workspace]/tasks/[workId]/+page.svelte",
      "the mirrors list: other tasks on the same source item, by title and read state",
    ],
  ]);

  it.each(["listWork", "getWork"])("%s always asks for it", (method) => {
    const offenders = [];
    for (const file of sources) {
      const body = readFileSync(join(root, "..", file), "utf8");
      const pattern = new RegExp(`\\.${method}\\(`, "g");
      for (const match of body.matchAll(pattern)) {
        // The call's arguments, up to the balanced close. Calls here are
        // short enough that the next `)` at depth zero is the end.
        let depth = 0;
        let end = match.index;
        for (let i = match.index + match[0].length - 1; i < body.length; i++) {
          if (body[i] === "(") depth += 1;
          if (body[i] === ")") {
            depth -= 1;
            if (depth === 0) {
              end = i;
              break;
            }
          }
        }
        const args = body.slice(match.index, end + 1);
        const name = file;
        /*
         * Keyed on the method as well as the file. Keyed on the file alone,
         * excusing the task page's mirrors `listWork` also excused its two
         * `getWork` calls — including the card read the whole page is built
         * on, which could then quietly drop the parameter and stay green.
         */
        if (
          !/summary:\s*1|SUMMARY_READ/.test(args) &&
          !NO_STATE_SHOWN.has(`${method} ${name}`)
        ) {
          offenders.push(`${method} ${name}: ${args.replace(/\s+/g, " ")}`);
        }
      }
    }
    expect(offenders).toEqual([]);
  });

  it("every read excused from the opt-in is still a real file", () => {
    const present = new Set(sources);
    for (const key of NO_STATE_SHOWN.keys()) {
      const name = key.slice(key.indexOf(" ") + 1);
      expect(present.has(name), `${key} is excused but missing`).toBe(true);
    }
  });
});

describe("workProse", () => {
  it("reads the body whichever field the response put it in", () => {
    // Opted in: the object takes `summary`, the body moves.
    expect(
      workProse({
        summary: { status: { state: "blocked" } },
        summary_text: "**Goal:** ship it",
      }),
    ).toBe("**Goal:** ship it");
    // Opted out, or an older core: the body is still at `summary`.
    expect(workProse({ summary: "**Goal:** ship it" })).toBe(
      "**Goal:** ship it",
    );
  });

  it("never returns a stringified object", () => {
    expect(workProse({ summary: { status: { state: "blocked" } } })).toBe("");
    expect(workProse({ summary: ["a"] })).toBe("");
    expect(workProse(null)).toBe("");
  });
});
