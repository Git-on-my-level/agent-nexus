import { describe, expect, it } from "vitest";
import {
  distinctEvidenceLinks,
  evidenceSources,
  observationHistory,
  shortNativeId,
} from "../../src/lib/pm/evidence.js";

const ISSUE = "https://github.com/org/repo/issues/208";
const read = (at, extra = {}) => ({
  id: `obs-${at}`,
  status: "reported",
  verification: "reported",
  observed_at: at,
  evidence: [
    { url: ISSUE, kind: "issue" },
    { url: `${ISSUE}#issuecomment-1`, kind: "comment" },
    { url: `${ISSUE}#issuecomment-2`, kind: "comment" },
    { url: `${ISSUE}#issuecomment-3`, kind: "comment" },
    { url: `${ISSUE}#issuecomment-4`, kind: "comment" },
  ],
  ...extra,
});
const work = {
  ref: "card:208",
  source: { authority: "github", native_id: "org/repo#208" },
};

describe("task evidence grouped by source", () => {
  it("turns four reads of five links into one source line and five links", () => {
    const observations = [
      read("2026-09-01T12:04:00Z"),
      read("2026-09-01T12:03:00Z"),
      read("2026-09-01T12:02:00Z"),
      read("2026-09-01T12:01:00Z"),
    ];
    const [source, ...rest] = evidenceSources(observations, work);
    expect(rest).toEqual([]);
    expect(source).toMatchObject({
      name: "GitHub #208",
      count: 4,
      reads: 4,
      lastAt: "2026-09-01T12:04:00Z",
    });
    expect(source.links.map((link) => link.label)).toEqual([
      "Issue #208",
      "Comment 1",
      "Comment 2",
      "Comment 3",
      "Comment 4",
    ]);
  });

  it("counts a failed read in history but not as an observation", () => {
    const observations = [
      {
        id: "e1",
        status: "error",
        observed_at: "2026-09-01T12:05:00Z",
        error: { code: "rate_limited", message: "Rate limited" },
      },
      read("2026-09-01T12:01:00Z"),
    ];
    const [source] = evidenceSources(observations, work);
    expect(source.reads).toBe(1);
    expect(source.latest.status).toBe("error");
    expect(source.lastAt).toBe("2026-09-01T12:01:00Z");
  });

  it("folds consecutive identical reads in the history", () => {
    const rows = observationHistory([
      read("4", { source_revision: "r2" }),
      read("3", { source_revision: "r1" }),
      read("2", { source_revision: "r1" }),
      read("1", { source_revision: "r1" }),
    ]);
    expect(rows.map((row) => [row.count, row.newest, row.oldest])).toEqual([
      [1, "4", "4"],
      [3, "3", "1"],
    ]);
  });

  it("folds consecutive identical failures in the history", () => {
    const fail = (at) => ({
      id: at,
      status: "error",
      observed_at: at,
      error: { message: "Rate limited" },
    });
    const rows = observationHistory([
      fail("3"),
      fail("2"),
      read("1"),
      fail("0"),
    ]);
    expect(
      rows.map((row) => (row.group ? `fail×${row.count}` : "read")),
    ).toEqual(["fail×2", "read", "fail×1"]);
  });

  it("keeps unlinked evidence once and drops unsafe urls to text", () => {
    const links = distinctEvidenceLinks([
      {
        status: "reported",
        evidence: [
          { summary: "Deployed to staging", kind: "git_revision" },
          { summary: "Deployed to staging", kind: "git_revision" },
          { url: "javascript:alert(1)", summary: "bad" },
        ],
      },
    ]);
    expect(links).toHaveLength(2);
    expect(links[0]).toMatchObject({
      href: "",
      label: "Revision · Deployed to staging",
    });
    expect(links[1].href).toBe("");
  });

  it("shortens native ids to the part a person says", () => {
    expect(shortNativeId("org/repo#208")).toBe("#208");
    expect(shortNativeId("MUL-12")).toBe("MUL-12");
    expect(shortNativeId("12")).toBe("#12");
    expect(shortNativeId("")).toBe("");
  });
});
