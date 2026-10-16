import { describe, expect, it } from "vitest";
import {
  countWaitingDecisions,
  decisionsForInitiative,
  findDecision,
  normalizeBrief,
} from "../../src/lib/brief.js";

const brief = normalizeBrief({
  decisions: [
    {
      ref: "inbox:curation-batch:k",
      inbox_item_id: "inbox:curation-batch:k",
      kind: "ask",
      title: "Grouped",
      subject: { ref: "card:head", title: "Head" },
      blocks: [],
      batch: {
        key: "k",
        count: 2,
        members: [
          {
            ref: "inbox:ask:1",
            inbox_item_id: "inbox:ask:1",
            title: "One",
            subject_ref: "card:head",
          },
          {
            ref: "inbox:ask:2",
            inbox_item_id: "inbox:ask:2",
            title: "Two",
            subject_ref: "card:other",
            blocks: [{ ref: "card:blocked", title: "Blocked" }],
          },
        ],
      },
    },
    {
      ref: "inbox:solo",
      inbox_item_id: "inbox:solo",
      kind: "ask",
      title: "Solo",
      subject: { ref: "card:solo", title: "Solo" },
      blocks: [{ ref: "card:solo-block", title: "Solo block" }],
    },
  ],
});

describe("brief batch decisions", () => {
  it("opens the group for a member inbox id or ref", () => {
    expect(findDecision(brief, "inbox:ask:2")?.inboxItemId).toBe(
      "inbox:curation-batch:k",
    );
    expect(findDecision(brief, "inbox:ask:1")?.ref).toBe(
      "inbox:curation-batch:k",
    );
    expect(findDecision(brief, "inbox:curation-batch:k")?.title).toBe(
      "Grouped",
    );
    expect(findDecision(brief, "missing")).toBeNull();
    expect(findDecision(brief, "")).toBeNull();
  });

  it("matches only authoritative event identities and rejects suffix or ambiguous matches", () => {
    const withSuffixes = normalizeBrief({
      decisions: [
        ...brief.decisions,
        {
          ref: "inbox:ask:thread-1:tenant:42",
          inbox_item_id: "inbox:ask:thread-1:tenant:42",
          kind: "ask",
          title: "Tenant 42 suffix",
        },
        {
          ref: "inbox:ask:thread-1:none:42",
          inbox_item_id: "inbox:ask:thread-1:none:42",
          kind: "ask",
          title: "Bare 42 suffix",
        },
        {
          ref: "inbox:ask:thread-1:req-9:evt-99",
          inbox_item_id: "inbox:ask:thread-1:req-9:evt-99",
          event_ref: "event:evt-99",
          kind: "ask",
          title: "Authoritative event ask",
        },
      ],
    });
    expect(findDecision(withSuffixes, "event:42")).toBeNull();
    expect(findDecision(withSuffixes, "event:2")).toBeNull();
    expect(findDecision(withSuffixes, "event:evt-99")?.title).toBe(
      "Authoritative event ask",
    );
    expect(findDecision(withSuffixes, "event:solo")?.title).toBe("Solo");
    expect(findDecision(withSuffixes, "event:missing")).toBeNull();

    const ambiguous = normalizeBrief({
      decisions: [
        {
          ref: "inbox:dup",
          inbox_item_id: "dup",
          kind: "ask",
          title: "First duplicate",
        },
        {
          ref: "inbox:dup",
          inbox_item_id: "dup",
          kind: "ask",
          title: "Second duplicate",
        },
      ],
    });
    expect(findDecision(ambiguous, "inbox:dup")).toBeNull();
    expect(findDecision(ambiguous, "event:dup")).toBeNull();
  });

  it("includes a batch when any member subject or block matches", () => {
    const ids = (ref) =>
      decisionsForInitiative(brief, ref).map(
        (decision) => decision.inboxItemId,
      );
    expect(ids("card:other")).toEqual(["inbox:curation-batch:k"]);
    expect(ids("card:blocked")).toEqual(["inbox:curation-batch:k"]);
    expect(ids("card:head")).toEqual(["inbox:curation-batch:k"]);
    expect(ids("card:solo")).toEqual(["inbox:solo"]);
    expect(ids("card:solo-block")).toEqual(["inbox:solo"]);
    expect(ids("card:nope")).toEqual([]);
  });

  it("drops every member of an answered batch from the waiting count", () => {
    const decisions = brief.decisions;
    expect(countWaitingDecisions(decisions, {})).toBe(2);
    expect(
      countWaitingDecisions(decisions, {
        "inbox:curation-batch:k": { status: "pending" },
      }),
    ).toBe(1);
    expect(
      countWaitingDecisions(decisions, {
        "inbox:ask:2": { status: "pending" },
      }),
    ).toBe(1);
    expect(
      countWaitingDecisions(decisions, {
        "inbox:solo": { status: "pending" },
      }),
    ).toBe(1);
  });
});
