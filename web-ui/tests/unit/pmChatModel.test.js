import { describe, expect, it } from "vitest";
import {
  bareDecisionIds,
  candidateDecisionIdsFromTurn,
  clockTime,
  decisionChipLabel,
  elapsedLabel,
  evidenceRefsForTurn,
  hasPendingTurn,
  isDecisionRecord,
  isNearBottom,
  linkifyDecisionIds,
  proposedDecisionIds,
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

describe("bare decision ids named in a PM reply", () => {
  // Shape of the live reply on conversation pm_8b2893d6975999f7aab2859a8771df5f.
  const ID_A = "pm_169191b0729af8b4018adec6f3467199";
  const ID_B = "pm_515c81523644ffc31aee7b65c941138d";
  const decision = (id) => ({
    id,
    status: "awaiting_answer",
    instruction: "Resolve the escalated P0 bar.",
    work_ref: "card:run-qa-bug-bash",
  });

  it("reads the ids the PM writes as bare tokens in prose", () => {
    expect(
      bareDecisionIds(
        `My proposal (${ID_A}): P1 for the capture candidate. Then ${ID_B}.`,
      ),
    ).toEqual([ID_A, ID_B]);
  });

  it("ignores an id that is already a ref, a link target, or the wrong shape", () => {
    expect(bareDecisionIds(`Filed as decision:${ID_A}.`)).toEqual([]);
    expect(bareDecisionIds(`See /inbox?item=decision:${ID_A}`)).toEqual([]);
    expect(bareDecisionIds(`[${ID_A}](/inbox)`)).toEqual([]);
    expect(
      bareDecisionIds("pm_short and pm_ZZ91b0729af8b4018adec6f3467199"),
    ).toEqual([]);
  });

  it("counts an id only once, and never twice with its decision ref", () => {
    expect(
      candidateDecisionIdsFromTurn({
        evidence_refs: [`decision:${ID_A}`],
        response: `Proposals ${ID_A} and ${ID_B}, and ${ID_B} again.`,
      }),
    ).toEqual([ID_B]);
  });

  it("accepts a record that looks like a decision and refuses one that does not", () => {
    expect(isDecisionRecord(decision(ID_A), ID_A)).toBe(true);
    expect(isDecisionRecord({ status: "answered" }, ID_A)).toBe(true);
    // A failed lookup, a turn, and a record answering for some other id.
    expect(isDecisionRecord(null, ID_A)).toBe(false);
    expect(isDecisionRecord({ id: ID_A, status: "delivered" }, ID_A)).toBe(
      false,
    );
    expect(isDecisionRecord(decision(ID_B), ID_A)).toBe(false);
  });

  it("shows a bare id as a proposal only once it resolves to a decision", () => {
    const turn = {
      evidence_refs: ["document:gds-launch-checklist"],
      response: `My proposal (${ID_A}): hold. Also ${ID_B}.`,
    };
    // Nothing fetched yet, then one resolved and one refused.
    expect(proposedDecisionIds(turn, {})).toEqual([]);
    expect(
      proposedDecisionIds(turn, { [ID_A]: decision(ID_A), [ID_B]: null }),
    ).toEqual([ID_A]);
  });

  it("keeps a recorded decision ref listed while its record is still loading", () => {
    expect(
      proposedDecisionIds(
        { evidence_refs: [`decision:${ID_A}`], response: "Filed." },
        {},
      ),
    ).toEqual([ID_A]);
  });

  it("replaces a resolved id in the prose with a short link, and leaves the rest", () => {
    const linked = linkifyDecisionIds(
      `My proposal (${ID_A}): hold. Unresolved ${ID_B}.`,
      (id) => (id === ID_A ? `/o/local/w/local/inbox?item=decision:${id}` : ""),
    );
    expect(linked).toBe(
      `My proposal ([Decision pm_169191b0](/o/local/w/local/inbox?item=decision:${ID_A} "${ID_A}")): ` +
        `hold. Unresolved ${ID_B}.`,
    );
    expect(decisionChipLabel(ID_A)).toBe("Decision pm_169191b0");
  });

  it("never rewrites an id inside a code span or a fenced block", () => {
    const source = `Run \`anx pm decision ${ID_A}\`.\n\n\`\`\`\n${ID_A}\n\`\`\`\n`;
    expect(linkifyDecisionIds(source, () => "/inbox?item=decision:x")).toBe(
      source,
    );
  });

  it("returns the body unchanged when there is nothing to link", () => {
    expect(linkifyDecisionIds("Plain answer.", () => "/inbox")).toBe(
      "Plain answer.",
    );
    expect(linkifyDecisionIds(null, () => "/inbox")).toBe("");
  });
});
