import { describe, expect, it } from "vitest";
import {
  freshness,
  safeSourceHref,
  receiptSignal,
  phaseGroups,
  filterWork,
} from "../../src/lib/pm/presentation.js";

describe("PM evidence presentation", () => {
  const now = Date.parse("2026-09-01T12:00:00Z");
  it("does not call a recent check progress or invent freshness without a policy", () => {
    expect(freshness({ observedAt: "2026-09-01T11:59:00Z" }, now).key).toBe(
      "unknown",
    );
    expect(freshness({ staleAfter: "2026-09-01T13:00:00Z" }, now).key).toBe(
      "unknown",
    );
    expect(
      freshness(
        {
          observedAt: "2026-09-01T11:59:00Z",
          staleAfter: "2026-09-01T13:00:00Z",
        },
        now,
      ).key,
    ).toBe("fresh");
  });
  it("keeps failure and stale last-good evidence visible", () => {
    expect(
      freshness(
        {
          observedAt: "2026-09-01T10:00:00Z",
          staleAfter: "2026-09-01T11:00:00Z",
        },
        now,
      ).key,
    ).toBe("stale");
    expect(
      freshness(
        {
          observedAt: "2026-09-01T10:00:00Z",
          staleAfter: "2026-09-01T13:00:00Z",
          error: "Access denied",
        },
        now,
      ),
    ).toMatchObject({ key: "error", label: "Refresh failed" });
  });
  it("shows malformed dates and future-clock observations as unknown", () => {
    expect(
      freshness({ observedAt: "invalid", staleAfter: "invalid" }, now).key,
    ).toBe("unknown");
    expect(
      freshness(
        {
          observedAt: "2026-09-01T13:00:00Z",
          staleAfter: "2026-09-01T14:00:00Z",
        },
        now,
      ).key,
    ).toBe("unknown");
  });
  it("only offers absolute http(s) source links", () => {
    for (const value of [
      "javascript:alert(1)",
      "data:text/html,test",
      "/work",
      "file:///tmp/a",
      "https://user:secret@example.test/",
    ])
      expect(safeSourceHref(value)).toBe("");
    expect(safeSourceHref("https://example.test/issues/1")).toBe(
      "https://example.test/issues/1",
    );
  });
  it("never treats delivery, acknowledgement, or an unknown receipt as outcome verification", () => {
    expect(receiptSignal("delivered").verified).toBe(false);
    expect(receiptSignal("acknowledged").verified).toBe(false);
    expect(receiptSignal("applied").verified).toBe(false);
    expect(receiptSignal("verified").verified).toBe(true);
    expect(receiptSignal("new_remote_state")).toMatchObject({
      label: "new_remote_state",
      verified: false,
    });
  });
  it("board and table retain the same records and unfamiliar phases", () => {
    const records = [
      { id: "a", phase: "active", title: "One" },
      { id: "b", phase: "vendor_pause", title: "Two" },
      { id: "c", title: "Three" },
    ];
    const filtered = filterWork(records, { q: "" });
    expect(
      phaseGroups(filtered)
        .flatMap((group) => group.items)
        .map((item) => item.id)
        .sort(),
    ).toEqual(["a", "b", "c"]);
    expect(
      phaseGroups(filtered).some((group) => group.key === "vendor_pause"),
    ).toBe(true);
    expect(filterWork(records, { q: "two" }).map((item) => item.id)).toEqual([
      "b",
    ]);
  });
});
