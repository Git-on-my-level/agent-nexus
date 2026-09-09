import { describe, expect, it } from "vitest";
import {
  buildInboxRows,
  filterMailbox,
  inboxRowBadge,
} from "../../src/lib/inboxMailbox.js";

describe("inbox mailboxes", () => {
  it("puts awaiting decisions, blocked tasks, and open inbox items in Needs you", () => {
    const rows = buildInboxRows({
      decisions: [
        {
          id: "d1",
          instruction: "Approve the cut",
          status: "awaiting_answer",
          work_ref: "card:one",
        },
      ],
      work: [
        {
          ref: "card:blocked",
          title: "Stuck task",
          phase: "blocked",
          source: { authority: "nexus" },
        },
      ],
      inboxItems: [
        {
          id: "in-1",
          title: "Need a reply",
          kind: "ask",
          related_refs: ["thread:t1"],
        },
      ],
    });
    expect(
      filterMailbox(rows, "needs-you")
        .map((row) => row.kind)
        .sort(),
    ).toEqual(["decision", "inbox", "task"]);
  });

  it("puts answered decisions, stale tasks, and grouped updates in Watching", () => {
    const now = Date.parse("2026-09-01T12:00:00Z");
    const rows = buildInboxRows({
      decisions: [
        {
          id: "d2",
          instruction: "Already answered",
          status: "delivered",
          work_ref: "card:one",
        },
      ],
      work: [
        {
          ref: "card:stale",
          title: "Stale task",
          phase: "in_progress",
          source: { authority: "github" },
          freshness: {
            status: "stale",
            last_observed_at: "2026-09-01T08:00:00Z",
            stale_after_seconds: 60,
          },
        },
      ],
      updates: [
        {
          group_ref: "topic:launch",
          display_name: "Launch",
          group_type: "topic",
          unread_count: 3,
          newest_event: { ts: "2026-09-01T11:00:00Z" },
        },
      ],
      now,
    });
    expect(
      filterMailbox(rows, "watching")
        .map((row) => row.kind)
        .sort(),
    ).toEqual(["decision", "task", "update"]);
  });

  it("keeps untouched tasks out of the Inbox entirely", () => {
    const now = Date.parse("2026-09-01T12:00:00Z");
    const rows = buildInboxRows({
      work: [
        {
          ref: "card:calm",
          title: "Nothing wrong here",
          phase: "in_progress",
          source: { authority: "github" },
          freshness: {
            status: "fresh",
            last_observed_at: "2026-09-01T11:55:00Z",
            stale_after_seconds: 3600,
          },
        },
      ],
      now,
    });
    expect(rows).toEqual([]);
    expect(filterMailbox(rows, "handled")).toEqual([]);
  });

  it("never puts a raw ref in a row's list line", () => {
    const rows = buildInboxRows({
      work: [
        {
          ref: "card:blocked",
          title: "Stuck task",
          phase: "blocked",
          source: { authority: "github" },
        },
      ],
    });
    expect(rows[0].source).toBe("GitHub");
    expect(rows[0].source).not.toContain("card:");
    expect(rows[0].ref).toBe("card:blocked");
  });
});

describe("inbox row badges", () => {
  it("says nothing when the badge would repeat the mailbox", () => {
    const rows = buildInboxRows({
      decisions: [
        {
          id: "d1",
          instruction: "Approve the cut",
          status: "awaiting_answer",
          work_ref: "card:one",
        },
      ],
      inboxItems: [{ id: "in-1", title: "Need a reply", kind: "ask" }],
    });
    for (const row of rows) {
      expect(inboxRowBadge(row)).toBeNull();
    }
  });

  it("badges blocked, loud severity, and an unreachable source", () => {
    const now = Date.parse("2026-09-01T12:00:00Z");
    const rows = buildInboxRows({
      work: [
        {
          ref: "card:blocked",
          title: "Stuck",
          phase: "blocked",
          source: { authority: "nexus" },
        },
        {
          ref: "card:broken",
          title: "Unreachable",
          phase: "in_progress",
          source: { authority: "github" },
          refresh: { last_error: "boom" },
        },
      ],
      inboxItems: [
        {
          id: "in-2",
          title: "Production is down",
          kind: "escalate",
          severity: "critical",
        },
      ],
      now,
    });
    const byTitle = Object.fromEntries(
      rows.map((row) => [row.title, inboxRowBadge(row, now)]),
    );
    expect(byTitle.Stuck).toEqual({ label: "Blocked", tone: "warn" });
    expect(byTitle.Unreachable).toEqual({
      label: "Can't reach GitHub",
      tone: "warn",
    });
    expect(byTitle["Production is down"]).toEqual({
      label: "Critical",
      tone: "danger",
    });
  });
});
