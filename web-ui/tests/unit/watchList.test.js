import { describe, expect, it } from "vitest";

import { normalizeBrief } from "../../src/lib/brief.js";
import {
  groupSections,
  isBlocked,
  isLagging,
  isStale,
  lineText,
  rowProgress,
  rowStatus,
  sectionCounts,
  sectionOf,
  statusParts,
  watchRows,
  watchSummary,
} from "../../src/lib/watchList.js";
import { mockupBrief } from "../fixtures/watchMockupSeed.js";

const NOW = Date.parse("2026-10-24T12:00:00Z");
const daysAgo = (n) => new Date(NOW - n * 86_400_000).toISOString();

/** A normalized-looking initiative row with only what the rules read. */
const row = (extra = {}) => ({
  ref: "card:x",
  title: "X",
  state: "moving",
  proposed: false,
  promise: null,
  pastPromiseFor: 0,
  lastEvidenceAt: daysAgo(1),
  progress: null,
  blockedCount: 0,
  waitingOnYouCount: 0,
  parentRef: null,
  ...extra,
  rollup: {
    total: 0,
    byState: {},
    pastPromise: 0,
    needingViewer: 0,
    children: [],
    workstreams: [],
    ...extra.rollup,
  },
});

describe("lagging", () => {
  it("lags on the initiative's own promise", () => {
    expect(isLagging(row({ state: "past_promise" }))).toBe(true);
  });

  it("does not lag on one late child", () => {
    expect(isLagging(row({ rollup: { total: 2, pastPromise: 1 } }))).toBe(
      false,
    );
    expect(isLagging(row({ rollup: { total: 1, pastPromise: 1 } }))).toBe(
      false,
    );
  });

  it("lags on two or more late children that are a third of them", () => {
    expect(isLagging(row({ rollup: { total: 6, pastPromise: 2 } }))).toBe(true);
    expect(isLagging(row({ rollup: { total: 21, pastPromise: 9 } }))).toBe(
      true,
    );
  });

  it("does not lag when late children are under a third", () => {
    expect(isLagging(row({ rollup: { total: 7, pastPromise: 2 } }))).toBe(
      false,
    );
    expect(isLagging(row({ rollup: { total: 30, pastPromise: 9 } }))).toBe(
      false,
    );
  });
});

describe("blocked", () => {
  it("counts core's blocked_count and children in the blocked phase", () => {
    expect(isBlocked(row({ blockedCount: 1 }))).toBe(true);
    expect(
      isBlocked(row({ rollup: { total: 3, byState: { blocked: 1 } } })),
    ).toBe(true);
    expect(
      isBlocked(row({ rollup: { total: 3, byState: { ready: 3 } } })),
    ).toBe(false);
  });
});

describe("stale", () => {
  it("is stale after seven days without evidence", () => {
    expect(isStale(row({ lastEvidenceAt: daysAgo(7) }), NOW)).toBe(true);
    expect(isStale(row({ lastEvidenceAt: daysAgo(6.9) }), NOW)).toBe(false);
  });

  it("is not stale with no evidence yet", () => {
    expect(isStale(row({ lastEvidenceAt: "" }), NOW)).toBe(false);
  });
});

describe("sectionOf", () => {
  it("takes the first section that matches, in reading order", () => {
    const all = {
      state: "past_promise",
      waitingOnYouCount: 1,
      blockedCount: 1,
      lastEvidenceAt: daysAgo(30),
      proposed: true,
    };
    expect(sectionOf(row(all), NOW)).toBe("needs_you");
    expect(sectionOf(row({ ...all, waitingOnYouCount: 0 }), NOW)).toBe(
      "lagging",
    );
    expect(
      sectionOf(row({ ...all, waitingOnYouCount: 0, state: "moving" }), NOW),
    ).toBe("blocked");
    expect(
      sectionOf(
        row({
          ...all,
          waitingOnYouCount: 0,
          state: "moving",
          blockedCount: 0,
        }),
        NOW,
      ),
    ).toBe("stale");
    expect(
      sectionOf(
        row({
          ...all,
          waitingOnYouCount: 0,
          state: "moving",
          blockedCount: 0,
          lastEvidenceAt: daysAgo(1),
        }),
        NOW,
      ),
    ).toBe("proposed");
    expect(sectionOf(row(), NOW)).toBe("moving");
    expect(sectionOf(row({ state: "no_promise" }), NOW)).toBe("moving");
    expect(sectionOf(row({ ...all, state: "done" }), NOW)).toBe("done");
  });

  it("needs you for a child waiting on the reader", () => {
    expect(sectionOf(row({ rollup: { needingViewer: 1 } }), NOW)).toBe(
      "needs_you",
    );
  });

  it("keeps a single late child out of lagging", () => {
    expect(sectionOf(row({ rollup: { total: 9, pastPromise: 1 } }), NOW)).toBe(
      "moving",
    );
  });
});

describe("watchRows on the mockup brief", () => {
  const brief = normalizeBrief(mockupBrief("2026-10-24T12:00:00Z"));
  const rows = watchRows(brief.initiatives, NOW);

  it("sorts by section, then core's order, children and loops never rows", () => {
    expect(rows.map((r) => [r.ref, r.section])).toEqual([
      ["card:capture", "needs_you"],
      ["card:margin", "needs_you"],
      ["card:security", "needs_you"],
      ["card:retention", "lagging"],
      ["card:firmware", "moving"],
      ["card:smarter", "proposed"],
      ["card:docs-site", "done"],
    ]);
    expect(groupSections(rows).map((s) => [s.key, s.rows.length])).toEqual([
      ["needs_you", 3],
      ["lagging", 1],
      ["moving", 1],
      ["proposed", 1],
      ["done", 1],
    ]);
  });

  it("counts on one basis: open initiatives", () => {
    expect(sectionCounts(rows).done).toBe(1);
    expect(watchSummary(rows).map((p) => p.text)).toEqual([
      "6 open",
      "3 need you",
      "1 lagging",
      "1 not started",
    ]);
  });

  it("marks the open count as a lower bound when core truncated", () => {
    expect(watchSummary(rows, { truncated: true })[0].text).toBe("6+ open");
  });
});

describe("row parts", () => {
  it("says what is late and stuck, then the next step", () => {
    const parts = statusParts(
      row({
        state: "past_promise",
        pastPromiseFor: 4 * 86_400,
        promise: { at: daysAgo(4), what: "CI on **PR 24163**" },
        blockedCount: 2,
        waitingOnYouCount: 1,
        rollup: { total: 21, pastPromise: 9, byState: { blocked: 1 } },
      }),
      NOW,
    );
    expect(lineText(parts)).toBe(
      "own promise 4d past · 9 of 21 past · 1 blocked · 1 open ask · next: CI on PR 24163 · Oct 20",
    );
    // The tagged part reads as the initiative page does: "4d past".
    expect(parts[0]).toMatchObject({
      prefix: "own promise",
      text: "4d past",
      field: "initiatives.past_promise_for",
    });
  });

  it("says how long a stale row has been quiet", () => {
    const parts = statusParts(row({ lastEvidenceAt: daysAgo(9) }), NOW);
    expect(parts.map((p) => p.text)).toEqual(["last evidence 9d ago"]);
    expect(
      rowStatus(
        { ...row({ lastEvidenceAt: daysAgo(9) }), section: "stale" },
        NOW,
      ).text,
    ).toBe("Quiet 9d");
  });

  it("draws the bar from core's progress, else children done", () => {
    expect(rowProgress(row({ progress: { done: 3, total: 7 } }))).toMatchObject(
      {
        done: 3,
        total: 7,
        field: "initiatives.progress",
      },
    );
    expect(
      rowProgress(
        row({
          rollup: { total: 4, byState: { done: 1, cancelled: 1, ready: 2 } },
        }),
      ),
    ).toMatchObject({
      done: 2,
      total: 4,
      field: "initiatives.rollup.by_state",
    });
    expect(rowProgress(row())).toBe(null);
  });

  it("names why a row is in its section on the right", () => {
    const at = (r) => rowStatus({ ...r, section: sectionOf(r, NOW) }, NOW);
    expect(at(row({ waitingOnYouCount: 1 }))).toMatchObject({
      text: "Waiting on you",
      decide: true,
    });
    expect(
      at(row({ state: "past_promise", pastPromiseFor: 7 * 86_400 })).text,
    ).toBe("7d past");
    expect(at(row({ blockedCount: 1 })).text).toBe("Blocked");
    expect(at(row({ proposed: true })).text).toBe("Not started");
    expect(at(row({ promise: { at: daysAgo(-2), what: "" } }))).toMatchObject({
      text: "On track",
      sub: { text: "due Oct 26" },
    });
    // A stated next step is dated on the status line, not again here.
    expect(
      at(row({ promise: { at: daysAgo(-2), what: "Ship" } })).sub,
    ).toBeUndefined();
    expect(at(row({ state: "done" })).text).toBe("Done");
  });
});
