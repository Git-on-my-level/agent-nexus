import { describe, expect, it } from "vitest";

import { createDecisionEpoch } from "../../src/lib/decisionEpoch.js";

/** A promise plus the handle that settles it. */
function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("createDecisionEpoch", () => {
  it("keeps a read that neither overlaps nor precedes a decision", () => {
    const decisions = createDecisionEpoch();
    const epoch = decisions.current();
    expect(decisions.isStale(epoch)).toBe(false);
  });

  it("invalidates a read that started before the decision", async () => {
    const decisions = createDecisionEpoch();
    const epoch = decisions.current();
    await decisions.during(async () => {});
    expect(decisions.isStale(epoch)).toBe(true);
  });

  it("invalidates a read that started while the decision was in flight", async () => {
    // The one the start-only counter let through: this read began after the
    // click, so it has the new value, but it still saw the pre-decision
    // world. Applying it restores the row the reader just decided.
    const decisions = createDecisionEpoch();
    const held = deferred();
    const decision = decisions.during(() => held.promise);

    const epoch = decisions.current();
    expect(decisions.isStale(epoch)).toBe(false);

    held.resolve();
    await decision;
    expect(decisions.isStale(epoch)).toBe(true);
  });

  it("keeps a read issued after the decision settles", async () => {
    // The reconciling read the surface runs straight after deciding has to
    // be applied, or the page keeps whatever it optimistically guessed.
    const decisions = createDecisionEpoch();
    await decisions.during(async () => {});
    const epoch = decisions.current();
    expect(decisions.isStale(epoch)).toBe(false);
  });

  it("invalidates overlapping reads even when the decision fails", async () => {
    const decisions = createDecisionEpoch();
    const held = deferred();
    const decision = decisions.during(() => held.promise);
    const epoch = decisions.current();

    held.reject(new Error("core said no"));
    await expect(decision).rejects.toThrow("core said no");
    expect(decisions.isStale(epoch)).toBe(true);
  });

  it("returns what the decision returned", async () => {
    const decisions = createDecisionEpoch();
    await expect(decisions.during(async () => "granted")).resolves.toBe(
      "granted",
    );
  });

  it("keeps separate decisions from cancelling each other out", async () => {
    // Two bumps per decision must never land a later read back on an earlier
    // value: the counter only moves forward.
    const decisions = createDecisionEpoch();
    const epoch = decisions.current();
    await decisions.during(async () => {});
    await decisions.during(async () => {});
    expect(decisions.isStale(epoch)).toBe(true);
    expect(decisions.current()).toBe(4);
  });
});
