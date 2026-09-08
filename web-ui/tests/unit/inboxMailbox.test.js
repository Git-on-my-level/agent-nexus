import { describe, expect, it } from "vitest";
import { buildInboxRows, filterMailbox } from "../../src/lib/inboxMailbox.js";

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
});
