import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { indexResolvedRefs, refChipModel } from "../../src/lib/refResolve.js";

/**
 * The initiative UI reads three backend shapes, and these are the serialized
 * ones core checks its own production output against. Importing them directly
 * is the point: a fixture invented here could drift from the wire without
 * anything failing, which is exactly how the card model ended up expecting a
 * string `health` when core had always sent `{status, reason}`.
 *
 * `contracts/fixtures/initiative-overview/README.md` says UI tests should do
 * this rather than invent card fields.
 */
const contractFixture = (name) =>
  JSON.parse(
    readFileSync(
      fileURLToPath(
        new URL(
          `../../../contracts/fixtures/initiative-overview/${name}`,
          import.meta.url,
        ),
      ),
      "utf8",
    ),
  );

const refs = contractFixture("refs.json");

describe("the ref preview reads core's serialized resolve response", () => {
  const resolved = indexResolvedRefs(refs);
  const context = { organizationSlug: "scaling", workspaceSlug: "anx" };
  const chip = (ref) => refChipModel(ref, resolved, context);

  it("shows the owner's name, not the actor ref", () => {
    expect(refs.items[0].owner).toBe("actor:actor-1");
    expect(chip("card:initiative").owner).toBe("Actor 1");
  });

  it("reads board and next step out of their objects", () => {
    expect(refs.items[0].board).toEqual({
      ref: "board:portfolio",
      title: "Portfolio",
    });
    expect(chip("card:initiative")).toMatchObject({
      board: "Portfolio",
      nextStep: "Build",
    });
  });

  it("carries priority, progress and last movement", () => {
    expect(chip("card:initiative")).toMatchObject({
      priority: "p1",
      progress: { done: 0, total: 1 },
      lastMovedAt: "2026-10-04T12:00:00Z",
    });
  });

  it("rebases the workspace-relative url core sends", () => {
    expect(refs.items[0].url).toBe("/tasks/initiative");
    expect(chip("card:initiative").href).toBe(
      "/o/scaling/w/anx/tasks/initiative",
    );
  });

  it("keeps the unresolvable row as a not-found chip", () => {
    expect(chip("card:unknown")).toMatchObject({
      resolvable: false,
      href: "",
    });
  });
});
