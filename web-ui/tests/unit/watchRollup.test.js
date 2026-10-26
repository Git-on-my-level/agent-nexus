import { describe, expect, it } from "vitest";

import { normalizeBrief } from "../../src/lib/brief.js";
import {
  bucketCounts,
  bucketOf,
  childSegments,
  childrenLine,
  decisionIndex,
  kidPromise,
  kidRows,
  needsYouItems,
  nextCell,
  nextLine,
  outcomeLine,
  promisesCell,
  rollupRows,
  rollupSummary,
  SEGMENT_CAP,
  segmentsLabel,
  shortCodes,
  stateSummary,
} from "../../src/lib/watchRollup.js";
import { mockupBrief } from "../fixtures/watchMockupSeed.js";

const NOW = Date.parse("2026-10-24T12:00:00Z");
const brief = normalizeBrief(mockupBrief("2026-10-24T12:00:00Z"));
const row = (ref) => brief.initiatives.find((r) => r.ref === ref);

describe("rollupRows", () => {
  it("lists top-level initiatives by what needs the reader", () => {
    const rows = rollupRows(brief.initiatives);
    expect(rows.map((r) => [r.ref, r.bucket])).toEqual([
      ["card:capture", "needs_you"],
      ["card:margin", "needs_you"],
      ["card:security", "needs_you"],
      ["card:retention", "slipping"],
      ["card:smarter", "moving"],
      ["card:firmware", "quiet"],
      ["card:docs-site", "done"],
    ]);
    expect(bucketCounts(rows)).toMatchObject({
      needs_you: 3,
      slipping: 1,
      moving: 1,
      quiet: 1,
      done: 1,
    });
    expect(rollupSummary(rows)).toBe(
      "7 initiatives · 29 cards rolled up into them",
    );
  });

  it("never makes a child or a loop a row", () => {
    const rows = rollupRows([
      ...brief.initiatives,
      { ...row("card:smarter"), ref: "card:kid", parentRef: "card:capture" },
      ...brief.loops,
    ]);
    expect(rows.map((r) => r.ref)).not.toContain("card:kid");
    expect(rows.map((r) => r.ref)).not.toContain("card:weekly-digest");
  });

  it("buckets a decision for the reader as needs you before slipping", () => {
    expect(
      bucketOf({ state: "past_promise", waitingOnYouCount: 1, rollup: {} }),
    ).toBe("needs_you");
    expect(
      bucketOf({
        state: "moving",
        waitingOnYouCount: 0,
        rollup: { needingViewer: 0, pastPromise: 2 },
      }),
    ).toBe("slipping");
    expect(bucketOf({ state: "done", waitingOnYouCount: 3, rollup: {} })).toBe(
      "done",
    );
  });
});

describe("children", () => {
  it("draws one segment per child, done first, needs you before not started", () => {
    expect(
      childSegments(row("card:capture").rollup).map((s) => s.tone),
    ).toEqual([
      "done",
      "moving",
      "moving",
      "slipping",
      "slipping",
      "needs",
      "needs",
      "not_started",
      "not_started",
    ]);
  });

  it("draws by share past the cap, or when core did not send every child", () => {
    const children = Array.from({ length: SEGMENT_CAP + 2 }, (_, i) => ({
      ref: `card:c${i}`,
      title: "",
      state: i % 2 ? "done" : "in_progress",
      needsViewer: false,
      pastPromise: false,
    }));
    const segs = childSegments({
      total: children.length,
      byState: { done: 9, in_progress: 9 },
      children,
    });
    expect(segs).toEqual([
      { tone: "done", flex: 9 },
      { tone: "moving", flex: 9 },
    ]);
    expect(
      childSegments({ total: 40, byState: { backlog: 40 }, children: [] }),
    ).toEqual([{ tone: "not_started", flex: 40 }]);
    expect(childSegments({ total: 0 })).toEqual([]);
  });

  it("says how many children and workstreams", () => {
    expect(childrenLine(row("card:capture").rollup)).toBe(
      "9 children in 3 workstreams",
    );
    expect(childrenLine(row("card:firmware").rollup)).toBe("1 child");
    expect(childrenLine(row("card:docs-site").rollup)).toBe("no children yet");
    expect(childrenLine()).toBe("no children yet");
  });

  it("lists workstreams as kid rows and counts the cards outside them", () => {
    const kids = kidRows(row("card:capture").rollup, NOW);
    expect(kids.workstreams).toBe(true);
    // Each workstream from its own rollup: cards, codes, state, promise, owner.
    expect(
      kids.rows.map((k) => [
        k.title,
        k.cards,
        k.codes,
        k.stateText,
        k.promise.text,
        k.next,
      ]),
    ).toEqual([
      [
        "Record lane & legacy retirement",
        9,
        "T1 T2 P86 P90",
        "4 moving · 1 review",
        "due Oct 25",
        { kind: "owner", ref: "actor-lead" },
      ],
      [
        "Speaker identification",
        4,
        "",
        "2 moving · 1 review",
        "10h past",
        { kind: "owner", ref: "actor-lead" },
      ],
      [
        "Custody & merging",
        8,
        "wal ×2 P87",
        "3 moving · 1 review",
        "—",
        { kind: "owner", ref: "actor-agents" },
      ],
    ]);
    // The rest of the breakdown is in the title, not lost.
    expect(kids.rows[0].stateTitle).toBe(
      "4 moving · 1 review · 2 ready · 1 backlog · 1 done",
    );
    expect(kids.otherCards).toBe(6);
    // A workstream with something waiting on the reader says so, and You.
    const margin = kidRows(row("card:margin").rollup, NOW).rows;
    const pricing = margin.find((k) => k.ref === "card:pricing-ladder");
    expect(pricing.stateText).toBe("1 needs you · 1 moving");
    expect(pricing.stateTone).toBe("needs");
    expect(pricing.next).toEqual({ kind: "you" });
    const reads = margin.find((k) => k.ref === "card:db-reads");
    expect([reads.codes, reads.stateText, reads.promise.text]).toEqual([
      "db-p1 db-p2 db-p3 db-p4 …",
      "3 blocked · 4 moving",
      "3 past",
    ]);
    // No workstreams: the children themselves, each saying who is next.
    const plain = kidRows(row("card:security").rollup, NOW);
    expect(plain.workstreams).toBe(false);
    expect(plain.rows.find((k) => k.ref === "card:plugins").next).toEqual({
      kind: "you",
    });
    expect(plain.rows.find((k) => k.ref === "card:plugins").stateText).toBe(
      "Blocked",
    );
  });

  it("falls back to a workstream's own phase when core did not expand it", () => {
    const kid = {
      ref: "card:w",
      title: "W",
      state: "ready",
      pastPromise: true,
      needsViewer: false,
      owner: null,
      rollup: null,
    };
    const [only] = kidRows(
      { total: 1, children: [kid], workstreams: [kid] },
      NOW,
    ).rows;
    expect([only.cards, only.codes, only.stateText, only.promise.text]).toEqual(
      [0, "", "Ready", "past promise"],
    );
    expect(only.next).toEqual({ kind: "none" });
  });

  it("reads a promise from the workstream's rollup, else the card's", () => {
    const at = (hours) => new Date(NOW + hours * 3_600_000).toISOString();
    const stream = (rollup) => ({
      rollup: { pastPromise: 0, earliestPromiseAt: null, ...rollup },
    });
    expect(kidPromise(stream({ pastPromise: 3 }), NOW).text).toBe("3 past");
    expect(kidPromise(stream({ earliestPromiseAt: at(-6) }), NOW)).toEqual({
      text: "6h past",
      tone: "warn",
      field: "initiatives.rollup.workstreams.rollup.earliest_promise_at",
    });
    expect(kidPromise(stream({ earliestPromiseAt: at(48) }), NOW).text).toBe(
      "due Oct 26",
    );
    expect(kidPromise(stream({}), NOW).text).toBe("—");
    expect(kidPromise({ doneAt: at(-30) }, NOW).text).toBe("done Oct 23");
  });

  it("folds repeated short codes and caps the list", () => {
    const codes = (...list) =>
      shortCodes({ children: list.map((shortCode) => ({ shortCode })) });
    expect(codes("p76", "p76", "p76")).toBe("P76 ×3");
    expect(codes("fs-p3", "", "cost", "t1")).toBe("fs-p3 cost T1");
    expect(codes("a1", "a2", "a3", "a4", "a5")).toBe("A1 A2 A3 A4 …");
    expect(shortCodes(null)).toBe("");
  });

  it("counts a child waiting on the reader once", () => {
    expect(
      stateSummary({
        byState: { blocked: 2, ready: 1 },
        needingViewer: 1,
        children: [{ state: "blocked", needsViewer: true }],
      }),
    ).toEqual({
      text: "1 needs you · 1 blocked",
      full: "1 needs you · 1 blocked · 1 ready",
      tone: "needs",
    });
  });
});

describe("outcome", () => {
  it("shows the outcome only when it says more than the title", () => {
    expect(outcomeLine(row("card:retention"))).toBe(
      "established-user retention up · target not set yet",
    );
    expect(outcomeLine({ title: "Ship it", outcome: "ship it " })).toBe("");
    expect(outcomeLine({ title: "Ship it", outcome: "**Ship** v2" })).toBe(
      "Ship v2",
    );
    expect(outcomeLine(row("card:firmware"))).toBe("");
  });
});

describe("row cells", () => {
  it("leads with the stated next step", () => {
    expect(nextLine(row("card:smarter"), NOW).map((p) => p.text)).toEqual([
      "next: readouts per experiment · Oct 26",
    ]);
    expect(nextLine(row("card:capture"), NOW).map((p) => p.text)).toEqual([
      "next: legacy queues gone, no silent loss",
      "10h past",
    ]);
    expect(nextLine(row("card:firmware"), NOW).map((p) => p.text)).toEqual([
      "no promise",
      "last evidence 3d ago",
    ]);
  });

  it("counts promises from the children, else the initiative's own", () => {
    expect(promisesCell(row("card:capture"))).toMatchObject({
      text: "2 of 9 past",
      tone: "warn",
    });
    expect(promisesCell(row("card:smarter")).text).toBe("on track");
    expect(promisesCell(row("card:retention"))).toMatchObject({
      text: "1d past",
      tone: "warn",
    });
    expect(promisesCell(row("card:docs-site")).text).toBe("done");
  });

  it("names the reader, else the owner, else nobody", () => {
    expect(nextCell(row("card:margin")).kind).toBe("you");
    expect(nextCell(row("card:smarter"))).toEqual({
      kind: "owner",
      ref: "actor-agents",
    });
    expect(nextCell(row("card:retention")).kind).toBe("none");
  });
});

describe("needsYouItems", () => {
  it("puts the reader's asks first and never repeats the child they cover", () => {
    const items = needsYouItems(row("card:capture"), decisionIndex(brief));
    expect(items.map((i) => [i.kind, i.ref])).toEqual([
      ["ask", "card:fallback-funding"],
      ["ask", "card:held-capture"],
    ]);
  });

  it("keeps a waiting child no ask covers", () => {
    const lane = {
      ...row("card:capture"),
      rollup: {
        ...row("card:capture").rollup,
        children: [
          ...row("card:capture").rollup.children,
          {
            ref: "card:extra",
            title: "Extra",
            state: "blocked",
            needsViewer: true,
            pastPromise: false,
          },
        ],
      },
    };
    expect(
      needsYouItems(lane, decisionIndex(brief)).map((i) => i.ref),
    ).toContain("card:extra");
  });
});

describe("screen-reader text and ask coverage", () => {
  it("says what the bar shows, tone by tone", () => {
    expect(segmentsLabel(row("card:capture").rollup)).toBe(
      "1 done, 2 in progress, 2 slipping, 2 need you, 2 not started",
    );
  });

  it("lets an ask that blocks a waiting child stand in for it", () => {
    const capture = row("card:capture");
    const lane = {
      ...capture,
      rollup: {
        ...capture.rollup,
        children: [
          ...capture.rollup.children,
          {
            ref: "card:blocked-kid",
            title: "Blocked kid",
            state: "blocked",
            needsViewer: true,
            pastPromise: false,
          },
        ],
      },
    };
    const withBlocks = {
      ...brief,
      decisions: [
        ...brief.decisions,
        {
          ...brief.decisions[0],
          ref: "inbox:ask-blocks",
          subject: { ref: "card:capture", title: "" },
          blocks: [{ ref: "card:blocked-kid", title: "Blocked kid" }],
        },
      ],
    };
    const items = needsYouItems(lane, decisionIndex(withBlocks));
    expect(items.map((i) => i.key)).toContain("inbox:ask-blocks");
    expect(items.map((i) => i.key)).not.toContain("card:blocked-kid");
  });
});
