import { describe, expect, it } from "vitest";

import { pendingDashboardPins } from "../../src/lib/mutationOutbox.js";

const pins = (key, createdAt, document_refs, state = "pending") => ({
  key,
  operation: "workspace.dashboard.pins.set",
  state,
  createdAt,
  request: { body: { request_key: key, document_refs } },
});

describe("pendingDashboardPins", () => {
  it("shows the newest pin set that may still be sent", () => {
    expect(
      pendingDashboardPins([
        pins("old", 1, ["document:a"], "retrying"),
        pins("new", 2, ["document:a", "document:b"], "sending"),
        {
          key: "move",
          operation: "cards.move",
          state: "pending",
          createdAt: 3,
        },
      ]),
    ).toEqual(["document:a", "document:b"]);
  });
  it("never shows a superseded or definitively failed set", () => {
    expect(
      pendingDashboardPins([
        pins("old", 1, ["document:a"], "superseded"),
        pins("new", 2, [], "attention"),
      ]),
    ).toBeNull();
  });
  it("keeps an empty set: unpinning everything is an edit too", () => {
    expect(pendingDashboardPins([pins("only", 1, [])])).toEqual([]);
  });
});
