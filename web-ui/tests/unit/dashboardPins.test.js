import { describe, expect, it } from "vitest";

import {
  MAX_DASHBOARD_PINS,
  PINS_OPERATION,
  applyPinOp,
  displayedPins,
  personalPins,
  pinEditWaits,
} from "../../src/lib/dashboardPins.js";

const edit = (key, order, op, ref, state = "pending") => ({
  key,
  operation: PINS_OPERATION,
  state,
  request: { op, ref, order, request_key: key },
});
const full = Array.from(
  { length: MAX_DASHBOARD_PINS },
  (_, i) => `document:d${i}`,
);

describe("applyPinOp", () => {
  it("is idempotent, so a replay lands on the same set", () => {
    const once = applyPinOp(["document:a"], { op: "add", ref: "document:b" });
    expect(once).toMatchObject({
      refs: ["document:a", "document:b"],
      changed: true,
    });
    expect(
      applyPinOp(once.refs, { op: "add", ref: "document:b" }),
    ).toMatchObject({
      refs: once.refs,
      changed: false,
    });
    expect(
      applyPinOp(["document:a"], { op: "remove", ref: "document:x" }),
    ).toMatchObject({ refs: ["document:a"], changed: false });
  });
  it("refuses a thirteenth pin but still removes when full", () => {
    expect(applyPinOp(full, { op: "add", ref: "document:new" })).toMatchObject({
      full: true,
      changed: false,
      refs: full,
    });
    expect(applyPinOp(full, { op: "add", ref: full[0] }).full).toBe(false);
    expect(applyPinOp(full, { op: "remove", ref: full[0] }).refs).toHaveLength(
      MAX_DASHBOARD_PINS - 1,
    );
  });
});

describe("personalPins", () => {
  it("counts the workspace default as no personal pins", () => {
    expect(
      personalPins({
        document_refs: ["document:a"],
        source: "workspace_default",
      }),
    ).toEqual([]);
    expect(personalPins(null)).toEqual([]);
    expect(
      personalPins({ document_refs: ["document:a"], source: "personal" }),
    ).toEqual(["document:a"]);
  });
});

describe("displayedPins", () => {
  it("applies saved edits on top of core's set, in the order made", () => {
    const rows = [
      edit("k3", 3, "remove", "document:b"),
      edit("k1", 1, "add", "document:b"),
      edit("k2", 2, "add", "document:c", "retrying"),
      edit("k4", 4, "add", "document:d", "attention"),
      { key: "m", operation: "cards.move", state: "pending", request: {} },
    ];
    expect(displayedPins(["document:a"], rows)).toEqual([
      "document:a",
      "document:c",
    ]);
  });
});

describe("pinEditWaits", () => {
  it("holds an edit behind an older sendable edit to the same dashboard", () => {
    const add = edit("k1", 1, "add", "document:b", "retrying");
    const remove = edit("k2", 2, "remove", "document:b");
    const other = edit("k3", 3, "add", "document:c");
    const rows = [add, remove, other];
    expect(pinEditWaits(remove, rows)).toBe(true);
    expect(pinEditWaits(add, rows)).toBe(false);
    // Different dashboards commute.
    expect(pinEditWaits(other, rows)).toBe(false);
    // A definitive failure needs the reader; it does not block newer intent.
    expect(pinEditWaits(remove, [{ ...add, state: "attention" }, remove])).toBe(
      false,
    );
  });
});
