import { describe, expect, it } from "vitest";

import briefFixture from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };
import { normalizeBrief } from "../../src/lib/brief.js";
import {
  changeLine,
  changeVerb,
  overTargetAsk,
} from "../../src/lib/watchRollup.js";
import { overTargetNotice, recentChanges } from "../../src/lib/watchList.js";

const NOW = Date.parse("2026-10-24T12:00:00Z");
const membership = (initiative, board = "board:tasks") => ({
  board_ref: board,
  initiative,
});
const change = (type, before, after, extra = {}) => ({
  ref: `event:${type}`,
  ts: "2026-10-24T09:00:00Z",
  type,
  actor_id: "codex",
  payload: {
    subject_ref: "card:battery",
    title: "Battery measurement",
    reason: "Independent outcome",
    before,
    after,
  },
  ...extra,
});
const normalized = (raw) =>
  normalizeBrief({ initiative_changes: { items: [raw], truncated: false } })
    .initiativeChanges.items[0];

describe("initiative set normalization", () => {
  it("reads the shared fixture's budget, over-target facts and changes", () => {
    const brief = normalizeBrief(briefFixture);
    expect(brief.initiativeBudget).toEqual({
      count: 6,
      target: 5,
      truncated: false,
    });
    expect(brief.overTarget).toEqual({
      count: 6,
      target: 5,
      truncated: false,
      ask: "Initiatives exceed the target: fold this initiative or keep it?",
      subjectRef: "card:battery-measurement",
    });
    expect(brief.initiativeChanges.items).toEqual([
      {
        ref: "event:initiative-added",
        ts: "2026-10-15T09:00:00Z",
        type: "initiative_promoted",
        actorId: "codex",
        subjectRef: "card:battery-measurement",
        title: "Battery measurement",
        reason: "Independent outcome",
        before: { boardRef: "board:tasks", initiative: false },
        after: { boardRef: "board:initiatives", initiative: true },
      },
    ]);
  });

  it("degrades for a core that predates the fields", () => {
    const brief = normalizeBrief({});
    expect(brief.initiativeBudget).toBeNull();
    expect(brief.overTarget).toBeNull();
    expect(brief.initiativeChanges).toEqual({ items: [], truncated: false });
  });

  it("keeps an unknown change type readable and drops rows with no subject", () => {
    expect(normalized(change("initiative_split", {}, {})).type).toBe("unknown");
    expect(
      normalizeBrief({
        initiative_changes: {
          items: [{ ref: "event:x", type: "initiative_promoted", payload: {} }],
        },
      }).initiativeChanges.items,
    ).toEqual([]);
  });
});

describe("budget", () => {
  it("treats a target of 0 as a real target", () => {
    const budget = normalizeBrief({
      initiative_budget: { count: 2, target: 0, truncated: false },
    }).initiativeBudget;
    expect(budget).toEqual({ count: 2, target: 0, truncated: false });
    const rows = [{ section: "moving" }, { section: "proposed" }];
    expect(overTargetNotice(rows, budget)).toEqual({ open: 2, target: 0 });
  });

  it("keeps the legacy default of 15 only for an absent target", () => {
    expect(
      normalizeBrief({ initiative_budget: { count: 3, truncated: false } })
        .initiativeBudget,
    ).toEqual({ count: 3, target: 15, truncated: false });
  });
});

describe("over target", () => {
  const over = {
    count: 6,
    target: 5,
    truncated: false,
    ask: "Initiatives exceed the target: fold this initiative or keep it?",
  };

  it("counts open initiatives against the target, with no lower bound", () => {
    const rows = [
      ...Array.from({ length: 6 }, () => ({ section: "moving" })),
      { section: "done" },
    ];
    const budget = { count: 9, target: 5, truncated: true };
    expect(overTargetNotice(rows, budget, over)).toEqual({
      open: 6,
      target: 5,
    });
    // Done rows never count; within target there is no notice.
    expect(overTargetNotice(rows.slice(1), { ...budget, target: 6 })).toBe(
      null,
    );
    // Core's flag alone is not the count: the same basis decides.
    expect(overTargetNotice(rows.slice(2), budget, over)).toBe(null);
    expect(overTargetNotice(rows, null, null)).toBe(null);
  });

  it("links nothing by title alone when core named no subject", () => {
    const decisions = [
      {
        kind: "ask",
        title: "Merge battery-v2 calibration?",
        subject: { ref: "card:battery" },
      },
      { kind: "ask", title: over.ask, inboxItemId: "ask-7" },
    ];
    // Without the recommended subject an ask cannot be verified as the
    // count question, so an exact title match still shows no link.
    expect(overTargetAsk(over, decisions)).toBeNull();
    expect(overTargetAsk(over, decisions.slice(0, 1))).toBeNull();
    expect(overTargetAsk({ ...over, ask: "" }, decisions)).toBeNull();
    expect(overTargetAsk(null, decisions)).toBeNull();
  });

  it("requires the recommended subject, then the newest re-ask", () => {
    const aboutCard = { ...over, subjectRef: "card:battery-measurement" };
    const reAsk = (inboxItemId, createdAt, subjectRef) => ({
      kind: "ask",
      title: over.ask,
      createdAt,
      inboxItemId,
      subject: { ref: subjectRef },
    });
    const otherSubject = reAsk(
      "ask-other",
      "2026-10-24T11:00:00Z",
      "card:calibration",
    );
    const older = reAsk(
      "ask-older",
      "2026-10-24T10:00:00Z",
      aboutCard.subjectRef,
    );
    const newer = reAsk(
      "ask-newer",
      "2026-10-24T11:30:00Z",
      aboutCard.subjectRef,
    );
    // The fixed question asked about another card is not the link.
    expect(overTargetAsk(aboutCard, [otherSubject])).toBeNull();
    expect(overTargetAsk(aboutCard, [otherSubject, older])?.inboxItemId).toBe(
      "ask-older",
    );
    // The brief lists decisions by decide-by, not age; the newest re-ask wins.
    expect(
      overTargetAsk(aboutCard, [older, newer, otherSubject])?.inboxItemId,
    ).toBe("ask-newer");
  });
});

describe("change lines", () => {
  it("names each kind of membership change", () => {
    const verb = (type, before, after) =>
      changeVerb(normalized(change(type, before, after)));
    expect(
      verb("initiative_promoted", membership(false), membership(true)),
    ).toBe("Added");
    expect(
      verb("initiative_retired", membership(true), membership(false)),
    ).toBe("Retired");
    expect(verb("initiative_moved", membership(false), membership(true))).toBe(
      "Moved in",
    );
    expect(verb("initiative_moved", membership(true), membership(false))).toBe(
      "Moved out",
    );
    expect(verb("initiative_moved", membership(true), membership(true))).toBe(
      "Moved",
    );
    expect(verb("initiative_split", {}, {})).toBe("Changed");
  });

  it("reads actor, reason and time", () => {
    const row = normalized(
      change("initiative_promoted", membership(false), membership(true)),
    );
    expect(changeLine(row, (id) => `agent ${id}`, NOW)).toEqual({
      verb: "Added",
      title: "Battery measurement",
      actor: "agent codex",
      reason: "Independent outcome",
      time: "3h ago",
    });
  });

  it("leaves an empty reason empty and names a missing actor", () => {
    const row = normalized(
      change("initiative_retired", membership(true), membership(false), {
        actor_id: "",
        payload: {
          subject_ref: "card:battery",
          title: "",
          reason: "",
          before: membership(true),
          after: membership(false),
        },
      }),
    );
    const line = changeLine(row, (id) => id, NOW);
    expect(line.reason).toBe("");
    expect(line.actor).toBe("unknown");
    expect(line.title).toBe("card:battery");
  });

  it("keeps only the last seven days of changes", () => {
    const items = [
      { ref: "a", ts: "2026-10-24T09:00:00Z" },
      { ref: "b", ts: "2026-10-17T12:00:00Z" },
      { ref: "c", ts: "2026-10-17T11:59:00Z" },
      { ref: "d", ts: "" },
    ];
    expect(
      recentChanges({ items, truncated: true }, NOW).map((c) => c.ref),
    ).toEqual(["a", "b"]);
    expect(recentChanges(null, NOW)).toEqual([]);
  });
});
