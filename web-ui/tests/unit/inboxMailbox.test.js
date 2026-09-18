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

describe("decisions addressed to someone else", () => {
  it("go to Watching with a badge, never Needs you", () => {
    const rows = buildInboxRows({
      decisions: [
        {
          id: "mine",
          status: "awaiting_answer",
          can_answer: true,
          instruction: "Mine",
          work_ref: "card:a",
        },
        {
          id: "theirs",
          status: "awaiting_answer",
          can_answer: false,
          instruction: "Theirs",
          work_ref: "card:a",
        },
      ],
    });
    const mine = rows.find((row) => row.id === "decision:mine");
    const theirs = rows.find((row) => row.id === "decision:theirs");
    expect(mine.mailbox).toBe("needs-you");
    expect(inboxRowBadge(mine)).toBeNull();
    expect(theirs.mailbox).toBe("watching");
    expect(inboxRowBadge(theirs)).toMatchObject({
      label: "Waiting on someone else",
    });
  });
});

describe("answered decisions without receipts", () => {
  it("stay in front of the reader when the actions list could not load", () => {
    const rows = buildInboxRows({
      decisions: [
        {
          id: "a",
          status: "answered",
          instruction: "Do it",
          work_ref: "card:x",
        },
      ],
      actions: [],
      receiptsUnavailable: true,
    });
    const row = rows.find((entry) => entry.id === "decision:a");
    expect(row.mailbox).toBe("needs-you");
    expect(inboxRowBadge(row)).toMatchObject({
      label: "Delivery state unknown",
    });
  });
});

describe("proposals core marks as void", () => {
  it("read as gone, already there, or changed since, and wait under Watching", () => {
    const rows = buildInboxRows({
      decisions: [
        {
          id: "gone",
          status: "awaiting_answer",
          can_answer: false,
          work_missing: true,
          instruction: "Move to done",
          work_ref: "card:deleted",
        },
        {
          id: "moot",
          status: "awaiting_answer",
          can_answer: true,
          already_at_target: true,
          instruction: "Move to done",
          work_ref: "card:a",
        },
        {
          id: "stale",
          status: "awaiting_answer",
          can_answer: true,
          target_current: false,
          instruction: "Move to done",
          work_ref: "card:a",
        },
      ],
    });
    const byId = (id) => rows.find((row) => row.id === `decision:${id}`);
    expect(byId("gone").mailbox).toBe("watching");
    expect(inboxRowBadge(byId("gone"))).toMatchObject({
      label: "Task no longer exists",
    });
    expect(byId("moot").mailbox).toBe("watching");
    expect(inboxRowBadge(byId("moot"))).toMatchObject({
      label: "Already there",
    });
    expect(byId("stale").mailbox).toBe("watching");
    expect(inboxRowBadge(byId("stale"))).toMatchObject({
      label: "Task changed since",
    });
  });
});

describe("approvals the reader still has to deliver", () => {
  it("sit in Needs you with a Deliver badge, only for the approver", () => {
    const decision = {
      id: "d1",
      status: "answered",
      actor_id: "maya",
      action_id: "a1",
      work_ref: "card:a",
      instruction: "move",
    };
    const action = {
      id: "a1",
      decision_id: "d1",
      status: "pending_delivery",
      deliverable: true,
      attempts: [],
    };
    const mine = buildInboxRows({
      decisions: [decision],
      actions: [action],
      currentActorId: "maya",
    }).find((row) => row.id === "decision:d1");
    expect(mine.mailbox).toBe("needs-you");
    expect(inboxRowBadge(mine)).toMatchObject({ label: "Deliver" });
    const theirs = buildInboxRows({
      decisions: [decision],
      actions: [action],
      currentActorId: "leo",
    }).find((row) => row.id === "decision:d1");
    expect(theirs.mailbox).toBe("watching");
  });
});

describe("acknowledging a delivery that failed before it was sent", () => {
  it("reads as an acknowledged failure, not a closed request", () => {
    const decision = {
      id: "d2",
      status: "answered",
      actor_id: "maya",
      action_id: "a2",
      work_ref: "card:gone",
      instruction: "move",
      work_missing: true,
    };
    const failedThenAcknowledged = {
      id: "a2",
      decision_id: "d2",
      status: "acknowledged",
      deliverable: false,
      attempts: [
        {
          status: "failed",
          started_at: "2026-09-13T10:00:00Z",
          finished_at: "2026-09-13T10:00:01Z",
          receipt: { status: "failed", detail: "The task no longer exists" },
        },
      ],
    };
    const row = buildInboxRows({
      decisions: [decision],
      actions: [failedThenAcknowledged],
      currentActorId: "maya",
    }).find((row) => row.id === "decision:d2");
    expect(row.status).toBe("acknowledged");
    const closedUnsent = {
      ...failedThenAcknowledged,
      id: "a3",
      attempts: [],
      closed_without_delivery: true,
    };
    const closedRow = buildInboxRows({
      decisions: [{ ...decision, id: "d3", action_id: "a3" }],
      actions: [closedUnsent],
      currentActorId: "maya",
    }).find((row) => row.id === "decision:d3");
    expect(closedRow.status).toBe("closed");
  });
});

describe("inbox mailbox row ids", () => {
  it("keeps a typed inbox id instead of prefixing inbox: twice", () => {
    const rows = buildInboxRows({
      inboxItems: [
        {
          id: "inbox:escalate:thread-gds-launch:evt:evt",
          title: "Escalate the bug bash",
          kind: "escalate",
        },
      ],
    });
    expect(rows.map((row) => row.id)).toEqual([
      "inbox:escalate:thread-gds-launch:evt:evt",
    ]);
  });
});

describe("inbox subject fallback labels", () => {
  it("names a card subject as Task when no work title is known", () => {
    const rows = buildInboxRows({
      inboxItems: [
        {
          id: "in-card",
          title: "Need a reply",
          kind: "ask",
          subject_ref: "card:card-1",
          related_refs: ["thread:t1"],
        },
      ],
    });
    const row = rows.find((item) => item.id === "inbox:in-card");
    expect(row.source).toBe("Task: card-1");
  });
});
