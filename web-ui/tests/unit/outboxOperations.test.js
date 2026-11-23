import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  KNOWN_OPERATIONS,
  UNKNOWN_OPERATION,
  isKnownOperation,
  revivable,
} from "../../src/lib/outboxOperations.js";
import { SKIP, SKIP_MANY } from "../../src/lib/standingRulingSend.js";

describe("operations this build can send", () => {
  it("lists every operation send() handles", () => {
    const source = readFileSync(
      new URL("../../src/lib/mutationOutbox.js", import.meta.url),
      "utf8",
    );
    const named = [
      ...source.matchAll(/entry\.operation === "([a-z._]+)"/g),
      ...source.matchAll(/\[([^\]]+)\]\.includes\(\s*entry\.operation/g),
    ].flatMap((match) =>
      match[1].includes('"')
        ? [...match[1].matchAll(/"([a-z._]+)"/g)].map((m) => m[1])
        : [match[1]],
    );
    expect(named.length).toBeGreaterThan(15);
    for (const operation of named)
      expect(KNOWN_OPERATIONS.has(operation), operation).toBe(true);
    expect(isKnownOperation(SKIP)).toBe(true);
    expect(isKnownOperation(SKIP_MANY)).toBe(true);
    expect(isKnownOperation("from.a.newer.build")).toBe(false);
  });

  it("revives only rows an older tab gave up on for lack of the operation", () => {
    const stuck = {
      operation: SKIP,
      state: "attention",
      failure: { detail: UNKNOWN_OPERATION },
    };
    expect(revivable(stuck)).toBe(true);
    expect(revivable({ ...stuck, operation: "from.a.newer.build" })).toBe(
      false,
    );
    expect(
      revivable({ ...stuck, failure: { detail: "Ask is no longer open" } }),
    ).toBe(false);
    expect(revivable({ ...stuck, state: "pending" })).toBe(false);
  });
});
