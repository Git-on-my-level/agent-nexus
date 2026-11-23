import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  KNOWN_OPERATIONS,
  UNKNOWN_OPERATION,
  isKnownOperation,
  revivable,
} from "../../src/lib/outboxOperations.js";
import * as standing from "../../src/lib/standingRulingSend.js";
import { PINS_OPERATION } from "../../src/lib/dashboardPins.js";

const { SKIP, SKIP_MANY } = standing;
const source = (file) =>
  readFileSync(new URL(`../../src/lib/${file}`, import.meta.url), "utf8");

describe("operations this build can send", () => {
  it("lists exactly the operations send() handles", () => {
    const outbox = source("mutationOutbox.js");
    const named = [
      ...outbox.matchAll(/entry\.operation === "([a-z._]+)"/g),
      ...outbox.matchAll(/\[([^\]]+)\]\.includes\(\s*entry\.operation/g),
    ].flatMap((match) =>
      match[1].includes('"')
        ? [...match[1].matchAll(/"([a-z._]+)"/g)].map((m) => m[1])
        : [match[1]],
    );
    expect(named.length).toBeGreaterThan(15);
    for (const operation of named)
      expect(KNOWN_OPERATIONS.has(operation), operation).toBe(true);
    // And the reverse: nothing is claimed that no sender handles, or a tab
    // would take work it cannot send, the failure this list exists to stop.
    const standingHandled = [
      ...source("standingRulingSend.js").matchAll(
        /entry\.operation === ([A-Z_]+)\)/g,
      ),
    ].map((match) => standing[match[1]]);
    expect(standingHandled).toContain(SKIP_MANY);
    const handled = new Set([...named, ...standingHandled]);
    if (/entry\.operation === PINS_OPERATION/.test(outbox))
      handled.add(PINS_OPERATION);
    for (const operation of KNOWN_OPERATIONS)
      expect(handled.has(operation), operation).toBe(true);
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
