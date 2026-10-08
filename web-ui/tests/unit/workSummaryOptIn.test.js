import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { globSync } from "node:fs";

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
  const sources = globSync("**/*.{svelte,js}", {
    cwd: root,
    withFileTypes: true,
  })
    .filter((entry) => entry.isFile())
    .map((entry) => `${entry.parentPath ?? entry.path}/${entry.name}`)
    .filter(
      (file) =>
        !file.includes("/generated/") &&
        !file.includes("/dev/") &&
        !file.endsWith("anxCoreClient.js"),
    )
    .sort();

  /**
   * Reads that show no card state, and so should not pay for one.
   *
   * Each one is here because it renders titles, counts or source identity
   * only. A read that grows a status badge has to drop out of this list, and
   * a reviewer can check that by looking at the file.
   */
  const NO_STATE_SHOWN = new Map([
    ["src/lib/inboxSources.js", "names the source behind an inbox row"],
    [
      "src/routes/o/[organization]/w/[workspace]/integrations/+page.svelte",
      "counts and lists tasks per connection; shows read freshness, not state",
    ],
    [
      "src/routes/o/[organization]/w/[workspace]/pm/+page.svelte",
      "picks a task by title for a PM conversation",
    ],
    [
      "src/routes/o/[organization]/w/[workspace]/tasks/[workId]/+page.svelte",
      "the mirrors list: other tasks on the same source item, by title and read state",
    ],
  ]);

  it.each(["listWork", "getWork"])("%s always asks for it", (method) => {
    const offenders = [];
    for (const file of sources) {
      const body = readFileSync(file, "utf8");
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
        const name = file.slice(root.length - 3);
        if (
          !/summary:\s*1|SUMMARY_READ/.test(args) &&
          !NO_STATE_SHOWN.has(name)
        ) {
          offenders.push(`${name}: ${args.replace(/\s+/g, " ")}`);
        }
      }
    }
    expect(offenders).toEqual([]);
  });

  it("every read excused from the opt-in is still a real file", () => {
    const present = new Set(sources.map((f) => f.slice(root.length - 3)));
    for (const name of NO_STATE_SHOWN.keys()) {
      expect(present.has(name), `${name} is excused but missing`).toBe(true);
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
