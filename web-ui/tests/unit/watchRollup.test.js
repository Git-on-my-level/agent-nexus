import { describe, expect, it } from "vitest";

import { normalizeBrief } from "../../src/lib/brief.js";
import {
  bucketCounts,
  bucketOf,
  childSegments,
  childrenLine,
  decisionIndex,
  kidRows,
  needsYouItems,
  nextCell,
  nextLine,
  promisesCell,
  rollupRows,
  rollupSummary,
  SEGMENT_CAP,
  segmentsLabel,
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
    expect(childrenLine(row("card:docs-site").rollup)).toBe("");
  });

  it("lists workstreams as kid rows and counts the cards outside them", () => {
    const kids = kidRows(row("card:capture").rollup);
    expect(kids.workstreams).toBe(true);
    expect(
      kids.rows.map((k) => [k.title, k.stateText, k.promise.text]),
    ).toEqual([
      ["Record lane & legacy retirement", "In progress", "—"],
      ["Speaker identification", "In progress", "past promise"],
      ["Custody & merging", "In review", "—"],
    ]);
    expect(kids.otherCards).toBe(6);
    // No workstreams: the children themselves, each saying who is next.
    const plain = kidRows(row("card:security").rollup);
    expect(plain.workstreams).toBe(false);
    expect(plain.rows.find((k) => k.ref === "card:plugins").next).toBe("You");
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
