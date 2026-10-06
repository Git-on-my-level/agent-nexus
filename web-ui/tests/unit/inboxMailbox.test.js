import { describe, expect, it } from "vitest";
import {
  buildInboxRows,
  filterMailbox,
  formatWait,
  inboxItemIsReminder,
  inboxItemNeedsResponse,
  inboxItemSubject,
  inboxRowBadge,
  rowMatchesWorkRef,
  shortIdLabel,
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

  it("shows the ask instead of its blocked card, before and after the answer", () => {
    const work = [
      {
        ref: "card:launch",
        id: "launch-id",
        title: "Ship the launch",
        phase: "blocked",
        next_actor: "human:operator",
        updated_at: "2026-10-04T10:00:00Z",
        source: { authority: "nexus" },
      },
    ];
    const ask = {
      id: "inbox:ask-1",
      kind: "ask",
      status: "open",
      subject_ref: "card:launch-id",
      title: "Which launch date?",
      related_refs: ["card:launch-id"],
    };
    const openRows = buildInboxRows({ work, inboxItems: [ask] });
    expect(filterMailbox(openRows, "needs-you").map((row) => row.kind)).toEqual(
      ["inbox"],
    );

    const completedRows = buildInboxRows({
      work,
      inboxItems: [
        {
          ...ask,
          id: "completed:ask-1",
          status: "completed",
          responded_at: "2026-10-04T11:00:00Z",
        },
      ],
    });
    expect(filterMailbox(completedRows, "needs-you")).toEqual([]);
    expect(
      filterMailbox(completedRows, "handled").map((row) => row.kind),
    ).toEqual(["inbox"]);
  });

  it("shows a blocked card again if it changed after its ask was answered", () => {
    const rows = buildInboxRows({
      work: [
        {
          ref: "card:launch",
          id: "launch-id",
          title: "Ship the launch",
          phase: "blocked",
          updated_at: "2026-10-04T12:00:00Z",
          source: { authority: "nexus" },
        },
      ],
      inboxItems: [
        {
          id: "completed:ask-1",
          kind: "ask",
          status: "completed",
          subject_ref: "card:launch-id",
          responded_at: "2026-10-04T11:00:00Z",
          related_refs: ["card:launch-id"],
        },
      ],
    });
    expect(filterMailbox(rows, "needs-you").map((row) => row.kind)).toEqual([
      "task",
    ]);
  });

  it("keeps a blocked card in Needs you when no ask names it", () => {
    const rows = buildInboxRows({
      work: [
        {
          ref: "card:blocked",
          title: "Stuck task",
          phase: "blocked",
          next_actor: "human:operator",
          source: { authority: "nexus" },
        },
      ],
    });
    expect(filterMailbox(rows, "needs-you").map((row) => row.kind)).toEqual([
      "task",
    ]);
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
  it("names a card subject as Task, humanizing the handle when no work title is known", () => {
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
    expect(row.source).toBe("Task: Card 1");
  });
});

describe("Needs you triage order", () => {
  const minutesAgo = (now, minutes) =>
    new Date(now - minutes * 60_000).toISOString();

  it("puts whoever has waited longest first, then the louder severity", () => {
    const now = Date.parse("2026-09-27T12:00:30Z");
    const rows = buildInboxRows({
      inboxItems: [
        {
          id: "fresh-critical",
          kind: "escalate",
          title: "Fresh but critical",
          severity: "critical",
          source_event_time: minutesAgo(now, 5),
        },
        {
          id: "old-ask",
          kind: "ask",
          title: "Waiting three hours",
          source_event_time: minutesAgo(now, 192),
        },
        {
          id: "same-minute-high",
          kind: "ask",
          title: "Same minute, high",
          severity: "high",
          source_event_time: minutesAgo(now, 5),
        },
      ],
      now,
    });
    expect(filterMailbox(rows, "needs-you").map((row) => row.item.id)).toEqual([
      "old-ask",
      "fresh-critical",
      "same-minute-high",
    ]);
  });

  it("formats waits the way an operator reads them", () => {
    expect(formatWait(20_000)).toBe("<1m");
    expect(formatWait(41 * 60_000)).toBe("41m");
    expect(formatWait((3 * 60 + 12) * 60_000)).toBe("3h 12m");
    expect(formatWait(3 * 60 * 60_000)).toBe("3h");
    expect(formatWait((2 * 24 + 4) * 60 * 60_000)).toBe("2d 4h");
    expect(formatWait(Number.NaN)).toBe("");
  });
});

describe("inbox names", () => {
  it("names the requester and falls back to a short id, never a raw UUID", () => {
    const rows = buildInboxRows({
      inboxItems: [
        {
          id: "named",
          kind: "ask",
          title: "Named",
          requester_actor_id: "actor-omar",
        },
        {
          id: "unknown",
          kind: "ask",
          title: "Unknown",
          requester_agent_id: "agent_6400c2d2-1111-2222-3333-444455556666",
        },
      ],
      actorName: (id) => (id === "actor-omar" ? "Omar Reed" : ""),
    });
    const named = rows.find((row) => row.item.id === "named");
    const unknown = rows.find((row) => row.item.id === "unknown");
    expect(named.requesterLabel).toBe("Omar Reed");
    expect(unknown.requesterLabel).toBe("agent 6400c2d2");
    expect(unknown.requester.id).toBe(
      "agent_6400c2d2-1111-2222-3333-444455556666",
    );
  });

  it("shortens only identifiers, not readable handles", () => {
    expect(shortIdLabel("actor:actor-gds-qa")).toBe("actor-gds-qa");
    expect(shortIdLabel("6400c2d2-1111-2222-3333-444455556666")).toBe(
      "id 6400c2d2",
    );
    expect(shortIdLabel("")).toBe("");
  });

  it("drops core's 'Human response recorded' prefix from handled rows", () => {
    const rows = buildInboxRows({
      inboxItems: [
        {
          id: "completed:e1",
          status: "completed",
          kind: "ask",
          title: "Human response recorded: Confirm quest path",
        },
      ],
    });
    expect(rows[0].title).toBe("Confirm quest path");
  });
});

describe("inbox subjects", () => {
  it("prefers the task an ask names over the project it was filed on", () => {
    const subject = inboxItemSubject(
      {
        subject_ref: "topic:vertical-slice",
        related_refs: ["thread:t1", "card:lock-hub-quest"],
      },
      {
        work: [
          {
            ref: "card:lock-hub-quest",
            title: "Lock hub quest path",
            phase: "in_progress",
          },
        ],
      },
    );
    expect(subject).toMatchObject({
      ref: "card:lock-hub-quest",
      kind: "card",
      noun: "Task",
      title: "Lock hub quest path",
      phaseLabel: "In progress",
    });
  });

  it("scopes rows to one task for ?work_ref= links", () => {
    const rows = buildInboxRows({
      decisions: [
        { id: "d1", status: "awaiting_answer", work_ref: "card:one" },
        { id: "d2", status: "awaiting_answer", work_ref: "card:two" },
      ],
      inboxItems: [
        {
          id: "i1",
          kind: "ask",
          title: "About one",
          related_refs: ["card:one"],
        },
      ],
    });
    expect(
      rows
        .filter((row) => rowMatchesWorkRef(row, "card:one"))
        .map((row) => row.id)
        .sort(),
    ).toEqual(["decision:d1", "inbox:i1"]);
  });
});

describe("update rows", () => {
  it("say what changed instead of a core noun and a bare count", () => {
    const rows = buildInboxRows({
      updates: [
        {
          group_ref: "board:studio",
          group_type: "board",
          display_name: "Studio board",
          unread_count: 3,
          newest_event: { ts: "2026-09-01T11:00:00Z" },
          events: [
            {
              id: "e3",
              type: "card_moved",
              actor_id: "actor-leo",
              payload: { column_key: "review" },
              refs: ["card:a"],
            },
            {
              id: "e2",
              type: "card_moved",
              actor_id: "actor-leo",
              payload: { column_key: "review" },
              refs: ["card:b"],
            },
            {
              id: "e1",
              type: "message_posted",
              actor_id: "actor-nina",
              payload: { text: "Looks good" },
            },
          ],
        },
      ],
      actorName: (id) =>
        ({ "actor-leo": "Leo Park", "actor-nina": "Nina Vale" })[id] || "",
    });
    const row = rows.find((item) => item.kind === "update");
    expect(row.source).toBe("Leo moved 2 tasks to review · Nina commented");
    expect(row.source).not.toMatch(/Board updates|Thread updates/);
    expect(inboxRowBadge(row)).toBeNull();
  });
});

describe("report review reminders", () => {
  /** A reminder exactly as `AddReportReviewReminder` stores it. */
  const reminder = (extra = {}) => ({
    id: "report-review:abc",
    kind: "report_review",
    subtype: "report_review_due",
    title:
      "Panel milestones on Dashboard is due for review: refresh it or convert it to live.",
    summary: "Panel milestones on Dashboard is due for review.",
    response_proposals: [],
    subject_ref: "document:dashboard",
    related_refs: ["document:dashboard"],
    panel_id: "milestones",
    revision_ref: "document_revision:dashboard-r1",
    ...extra,
  });

  it("asks nobody for a response", () => {
    // Core sends no proposals and has no responder waiting: Reply and
    // Acknowledge were two buttons that could only fail.
    expect(inboxItemIsReminder(reminder())).toBe(true);
    expect(inboxItemNeedsResponse(reminder())).toBe(false);
    // An ordinary ask is untouched.
    expect(inboxItemNeedsResponse({ id: "a", kind: "ask" })).toBe(true);
  });

  it("stays in front of its author rather than filing itself as handled", () => {
    // Needing no response is not the same as being dealt with: the panel it
    // names is still the author's to refresh.
    const rows = buildInboxRows({ inboxItems: [reminder()] });
    expect(filterMailbox(rows, "needs-you").map((row) => row.item.id)).toEqual([
      "report-review:abc",
    ]);
    expect(filterMailbox(rows, "handled")).toEqual([]);
  });

  it("moves to Handled once core marks it complete", () => {
    const rows = buildInboxRows({
      inboxItems: [reminder({ completed_at: "2026-10-06T12:00:00Z" })],
    });
    expect(filterMailbox(rows, "needs-you")).toEqual([]);
    expect(filterMailbox(rows, "handled").map((row) => row.item.id)).toEqual([
      "report-review:abc",
    ]);
  });

  it("points at the dashboard it is about", () => {
    // "Open dashboard" needs a subject, and the reminder's is the document.
    const subject = inboxItemSubject(reminder(), {});
    expect(subject.kind).toBe("document");
    expect(subject.ref).toBe("document:dashboard");
  });
});
