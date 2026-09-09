import { describe, expect, it } from "vitest";
import {
  clockTime,
  elapsedLabel,
  evidenceRefsForTurn,
  hasPendingTurn,
  isNearBottom,
  refsFromResponse,
  startsTimeGroup,
  turnState,
} from "../../src/lib/pm/chatModel.js";

describe("PM chat presentation model", () => {
  it("finds typed refs named in the reply body and strips sentence punctuation", () => {
    expect(
      refsFromResponse(
        "Blocked on card:gds-bug-bash, see document:launch-checklist. Proposed as decision:d-1.",
      ),
    ).toEqual([
      "card:gds-bug-bash",
      "document:launch-checklist",
      "decision:d-1",
    ]);
  });

  it("does not mistake a bare URL for a typed ref", () => {
    expect(refsFromResponse("See https://example.test/issues/12")).toEqual([]);
  });

  it("unions recorded evidence with refs named in prose and drops decisions", () => {
    expect(
      evidenceRefsForTurn({
        evidence_refs: ["card:release", "decision:d-1"],
        response: "Also card:release and thread:standup, plus decision:d-2.",
      }),
    ).toEqual(["card:release", "thread:standup"]);
  });

  it("opens a time group at the head, and only after a five minute gap", () => {
    const first = { created_at: "2026-09-09T10:00:00Z" };
    const soon = { created_at: "2026-09-09T10:02:00Z" };
    const later = { created_at: "2026-09-09T10:08:00Z" };
    expect(startsTimeGroup(first, null)).toBe(true);
    expect(startsTimeGroup(soon, first)).toBe(false);
    expect(startsTimeGroup(later, soon)).toBe(true);
    // A missing instant is not a gap.
    expect(startsTimeGroup({}, first)).toBe(false);
  });

  it("returns no clock time for a missing or unparseable instant", () => {
    expect(clockTime("")).toBe("");
    expect(clockTime("not a date")).toBe("");
    expect(clockTime("2026-09-09T10:00:00Z")).toMatch(/^\d{2}:\d{2}$/);
  });

  it("formats an elapsed wait, and refuses an unknown duration", () => {
    expect(elapsedLabel(0)).toBe("0s");
    expect(elapsedLabel(42_000)).toBe("42s");
    expect(elapsedLabel(102_000)).toBe("1m 42s");
    expect(elapsedLabel(7_500_000)).toBe("2h 05m");
    expect(elapsedLabel(Number.NaN)).toBe("");
  });

  it("reads an answered turn as answered even when the status still says sending", () => {
    expect(
      turnState({ status: "sending", response: "Here is the state." }),
    ).toMatchObject({ kind: "answered" });
  });

  it("names a pending turn stalled only after the 45s runner threshold", () => {
    const now = Date.parse("2026-09-09T10:01:00Z");
    const fresh = turnState(
      { status: "sending", created_at: "2026-09-09T10:00:40Z" },
      now,
    );
    expect(fresh).toMatchObject({ kind: "pending", stalled: false });
    expect(fresh.elapsed).toBe("20s");
    expect(
      turnState({ status: "sending", created_at: "2026-09-09T10:00:00Z" }, now),
    ).toMatchObject({ kind: "pending", stalled: true, elapsed: "1m 00s" });
    // Without a created_at there is no elapsed wait to claim, and no stall.
    expect(turnState({ status: "sending" }, now)).toMatchObject({
      kind: "pending",
      elapsed: "",
      stalled: false,
    });
  });

  it("keeps failure detail and delivery uncertainty separable", () => {
    expect(
      turnState({ status: "failed", failure: "Runner lease expired" }),
    ).toMatchObject({ kind: "failed", detail: "Runner lease expired" });
    expect(turnState({ status: "unknown" })).toMatchObject({
      kind: "unknown",
    });
  });

  it("runs the elapsed clock only while a turn is still waiting", () => {
    const now = Date.parse("2026-09-09T10:01:00Z");
    expect(
      hasPendingTurn([{ status: "delivered", response: "Answered" }], now),
    ).toBe(false);
    expect(
      hasPendingTurn(
        [{ status: "delivered", response: "Answered" }, { status: "sending" }],
        now,
      ),
    ).toBe(true);
    expect(hasPendingTurn([{ status: "failed" }], now)).toBe(false);
  });

  it("sticks to the bottom within 64px and releases beyond it", () => {
    expect(
      isNearBottom({ scrollHeight: 1000, scrollTop: 940, clientHeight: 20 }),
    ).toBe(true);
    expect(
      isNearBottom({ scrollHeight: 1000, scrollTop: 500, clientHeight: 200 }),
    ).toBe(false);
    expect(isNearBottom(null)).toBe(true);
  });
});
