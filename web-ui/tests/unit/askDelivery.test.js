import { describe, expect, it } from "vitest";
import {
  INBOX_RESPONSE_OUTCOMES,
  askDeliveryModel,
  askEvidenceModel,
  askIsStale,
  askRefForInboxItem,
  deliveryReasonText,
  deliveryRowModel,
  isInboxResponseOutcome,
  linkifyAskEvidence,
  safeEvidenceUrl,
  subscriptionKindLabel,
  supportsNeedsContext,
  taskOutcomeModel,
} from "../../src/lib/askDelivery.js";
import { buildInboxRows, inboxRowBadge } from "../../src/lib/inboxMailbox.js";
import { createInboxOrder } from "../../src/lib/inboxOrder.js";

const NOW = Date.parse("2026-10-09T12:00:00.000Z");

describe("response outcomes", () => {
  it("accepts every outcome the generated contract enum carries", () => {
    expect(INBOX_RESPONSE_OUTCOMES).toContain("needs_context");
    expect(INBOX_RESPONSE_OUTCOMES).toContain("resolved");
    for (const outcome of INBOX_RESPONSE_OUTCOMES) {
      expect(isInboxResponseOutcome(outcome)).toBe(true);
    }
    expect(isInboxResponseOutcome("nonsense")).toBe(false);
    expect(isInboxResponseOutcome(undefined)).toBe(false);
  });

  it("offers a context request unless core restricts the outcomes", () => {
    expect(supportsNeedsContext({})).toBe(true);
    expect(supportsNeedsContext({ allowed_response_outcomes: [] })).toBe(true);
    expect(
      supportsNeedsContext({
        allowed_response_outcomes: ["approved", "rejected"],
      }),
    ).toBe(false);
    expect(
      supportsNeedsContext({
        allowed_response_outcomes: ["answered", "needs_context"],
      }),
    ).toBe(true);
  });
});

describe("ask ref for an inbox item", () => {
  it("prefers the request event ref and falls back to the source event id", () => {
    expect(askRefForInboxItem({ request_event_ref: "event:ask-1" })).toBe(
      "event:ask-1",
    );
    expect(askRefForInboxItem({ source_event_ref: "event:ask-2" })).toBe(
      "event:ask-2",
    );
    expect(askRefForInboxItem({ source_event_id: "ask-3" })).toBe(
      "event:ask-3",
    );
    // A ref that is not an event ref is not an ask id.
    expect(askRefForInboxItem({ request_event_ref: "card:abc" })).toBe("");
    expect(askRefForInboxItem({})).toBe("");
  });
});

describe("delivery state mapping", () => {
  it("names each state with its tone and keeps core's reason", () => {
    expect(
      deliveryRowModel(
        {
          id: "sub_1",
          kind: "webhook",
          label: "release bot",
          state: "failed",
          attempts: 5,
          reason: "dead_letter: retry_limit",
          last_at: "2026-10-09T10:00:00.000Z",
        },
        { now: NOW },
      ),
    ).toMatchObject({
      kindLabel: "Webhook",
      label: "release bot",
      stateLabel: "Failed",
      tone: "danger",
      failed: true,
      attempts: 5,
      reason: "Gave up after the attempt limit",
      ageLabel: "2h",
    });
    expect(
      deliveryRowModel({ state: "delivered" }, { now: NOW }),
    ).toMatchObject({
      stateLabel: "Delivered",
      tone: "ok",
      failed: false,
      ageLabel: "",
    });
    expect(deliveryRowModel({ state: "pending" }, { now: NOW })).toMatchObject({
      stateLabel: "Pending",
      tone: "neutral",
    });
    expect(deliveryRowModel({}, { now: NOW })).toMatchObject({
      state: "none",
      stateLabel: "Not delivered",
      tone: "neutral",
    });
  });

  it("keeps a state and a kind it has never seen rather than blanking them", () => {
    const row = deliveryRowModel({ kind: "carrier-pigeon", state: "queued" });
    expect(row.stateLabel).toBe("queued");
    expect(row.tone).toBe("neutral");
    expect(row.kindLabel).toBe("carrier-pigeon");
    expect(subscriptionKindLabel("")).toBe("Subscription");
  });

  it("does not answer a prototype key with a function", () => {
    // `state: "constructor"` used to resolve to Object and render a blank badge.
    expect(deliveryRowModel({ state: "constructor" }).stateLabel).toBe(
      "constructor",
    );
    expect(subscriptionKindLabel("constructor")).toBe("constructor");
    expect(deliveryReasonText("toString")).toBe("toString");
  });

  it("turns each of core's reason tokens into a sentence", () => {
    for (const [token, expected] of [
      ["recipient_inactive", "The subscribing agent is no longer active"],
      ["endpoint_blocked", "The endpoint address is not allowed"],
      ["transport_failed", "The endpoint could not be reached"],
      ["dead_letter: invalid_endpoint", "The endpoint URL is not usable"],
    ]) {
      expect(deliveryReasonText(token)).toBe(expected);
    }
    // A token this list has never seen is shown, not hidden.
    expect(deliveryReasonText("something_new")).toBe("something_new");
    expect(deliveryReasonText("")).toBe("");
  });
});

describe("task outcome", () => {
  it("names a context request rather than calling the task answered", () => {
    expect(
      taskOutcomeModel(
        { card_ref: "card:anx-7", phase: "ready", next_actor: "codex" },
        { status: "needs_context" },
      ).label,
    ).toBe("Returned for context · next: codex");
  });

  it("reads unblocked, closed and source-owned from core's record", () => {
    expect(
      taskOutcomeModel(
        { card_ref: "card:anx-7", phase: "ready", next_actor: "actor:dev" },
        { nextActorLabel: "Dana" },
      ),
    ).toMatchObject({ label: "Unblocked · next: Dana", closed: false });
    expect(
      taskOutcomeModel({ card_ref: "card:anx-7", phase: "done" }),
    ).toMatchObject({ label: "Closed", closed: true });
    expect(
      taskOutcomeModel({
        card_ref: "card:anx-7",
        phase: "in_progress",
        next_actor: "jira-bot",
        reason: "source_owned",
      }).label,
    ).toBe("Phase unchanged (source owns it) · next: jira-bot");
    expect(taskOutcomeModel(null)).toBeNull();
    expect(taskOutcomeModel({})).toBeNull();
  });
});

describe("ask delivery model", () => {
  const answered = {
    ask_id: "event:ask-1",
    status: "answered",
    is_stale: false,
    task_outcome: {
      card_ref: "card:anx-7",
      phase: "ready",
      next_actor: "actor:dev",
    },
    delivery: [],
  };

  it("names a missing subscriber as information, not an error", () => {
    const model = askDeliveryModel(answered, {
      now: NOW,
      nextActorLabel: "Dana",
    });
    expect(model.notDelivered).toBe(
      "Not delivered: no subscriber; task unblocked for Dana.",
    );
    expect(model.subscriptions).toEqual([]);
  });

  it("says the task was closed when no subscriber was waiting", () => {
    const model = askDeliveryModel({
      ...answered,
      task_outcome: { card_ref: "card:anx-7", phase: "done" },
    });
    expect(model.notDelivered).toBe(
      "Not delivered: no subscriber; the task was closed.",
    );
  });

  it("says nothing about a task when core recorded no task outcome", () => {
    // An access-grant decision, a legacy non-card ask and every answer older
    // than ask delivery all arrive without one. "The task was unblocked" there
    // asserts a task that does not exist.
    const model = askDeliveryModel({
      ask_id: "event:ask-1",
      status: "answered",
      is_stale: false,
      delivery: [],
    });
    expect(model.task).toBeNull();
    expect(model.notDelivered).toBe(
      "Not delivered: no subscriber was registered.",
    );
  });

  it("lists subscriptions and never claims no subscriber when there is one", () => {
    const model = askDeliveryModel(
      {
        ...answered,
        delivery: [
          { id: "sub_1", kind: "await", label: "cli", state: "delivered" },
          {
            id: "sub_2",
            kind: "bridge",
            label: "host",
            state: "failed",
            reason: "command exited 1",
          },
        ],
      },
      { now: NOW },
    );
    expect(model.notDelivered).toBeNull();
    expect(model.subscriptions.map((row) => row.kindLabel)).toEqual([
      "Live await",
      "Host bridge",
    ]);
    expect(model.subscriptions[1]).toMatchObject({
      failed: true,
      tone: "danger",
    });
  });

  it("leaves an open ask without a delivery verdict", () => {
    const model = askDeliveryModel({
      ask_id: "event:ask-1",
      status: "open",
      is_stale: true,
      delivery: [],
    });
    expect(model.notDelivered).toBeNull();
    expect(model.isStale).toBe(true);
    expect(model.needsContext).toBe(false);
  });

  it("marks a context request as not answered", () => {
    expect(
      askDeliveryModel({ status: "needs_context", delivery: [] }).needsContext,
    ).toBe(true);
  });

  it("renders an evidence noun for a prototype-shaped ref prefix", () => {
    expect(
      askEvidenceModel({ item: { related_refs: ["constructor:x"] } }).refs[0]
        .noun,
    ).toBe("constructor");
  });

  it("returns nothing for a read that produced nothing", () => {
    expect(askDeliveryModel(null)).toBeNull();
  });
});

describe("evidence", () => {
  it("rejects a URL that is not a credential-free http(s) one", () => {
    expect(safeEvidenceUrl("https://example.org/pull/1")).toBe(
      "https://example.org/pull/1",
    );
    expect(safeEvidenceUrl("javascript:alert(1)")).toBe("");
    expect(safeEvidenceUrl("https://user:pass@example.org/x")).toBe("");
    expect(safeEvidenceUrl("not a url")).toBe("");
    expect(safeEvidenceUrl("")).toBe("");
  });

  it("splits typed refs from labelled links and drops plumbing refs", () => {
    const model = askEvidenceModel({
      item: {
        subject_ref: "card:anx-7",
        related_refs: [
          "card:anx-7",
          "thread:t-1",
          "inbox:ask:1",
          "event:ask-1",
          "document:rulings",
        ],
      },
      event: {
        payload: {
          related_refs: ["document:rulings", "card:anx-9"],
          evidence: [
            { label: "Change", url: "https://example.org/repo/pull/123" },
            { label: "Bad", url: "javascript:alert(1)" },
          ],
          supersedes: "event:ask-0",
          authoring_override_reason: "urgent",
        },
      },
    });
    expect(model.refs.map((entry) => entry.ref)).toEqual([
      "document:rulings",
      "card:anx-9",
    ]);
    expect(model.refs[0]).toMatchObject({
      prefix: "document",
      id: "rulings",
      noun: "Document",
    });
    expect(model.links).toEqual([
      {
        label: "Change",
        url: "https://example.org/repo/pull/123",
        host: "example.org",
        kind: "Pull request",
      },
    ]);
    expect(model.supersedes).toBe("event:ask-0");
    expect(model.overrideReason).toBe("urgent");
    expect(model.empty).toBe(false);
  });

  it("falls back to the row's refs when the ask event cannot be read", () => {
    const model = askEvidenceModel({
      item: { subject_ref: "card:anx-7", related_refs: ["document:rulings"] },
      event: null,
    });
    expect(model.refs.map((entry) => entry.ref)).toEqual(["document:rulings"]);
    expect(model.links).toEqual([]);
  });

  it("is empty for an item with nothing but plumbing", () => {
    expect(
      askEvidenceModel({ item: { related_refs: ["thread:t-1"] } }).empty,
    ).toBe(true);
    expect(askEvidenceModel({}).empty).toBe(true);
  });
});

describe("linking names the ask already backed with evidence", () => {
  const evidence = {
    refs: [{ ref: "document:rulings", prefix: "document", id: "rulings" }],
    links: [
      {
        label: "Change",
        url: "https://example.org/repo/pull/123",
        host: "example.org",
        kind: "Pull request",
      },
    ],
  };
  const hrefFor = (ref) =>
    ref === "document:rulings" ? "/o/a/w/b/docs/rulings" : "";

  it("links a quoted name, a backticked name and a named PR", () => {
    expect(
      linkifyAskEvidence('See doc "rulings" before PR #123 lands.', evidence, {
        hrefFor,
      }),
    ).toBe(
      "See doc [rulings](/o/a/w/b/docs/rulings) before [PR #123](https://example.org/repo/pull/123) lands.",
    );
    expect(
      linkifyAskEvidence("Read doc `rulings`.", evidence, { hrefFor }),
    ).toBe("Read doc [`rulings`](/o/a/w/b/docs/rulings).");
  });

  it("leaves a name with no matching evidence alone", () => {
    expect(
      linkifyAskEvidence('doc "other" and PR #999', evidence, { hrefFor }),
    ).toBe('doc "other" and PR #999');
  });

  it("leaves a ref it cannot route as plain text", () => {
    expect(
      linkifyAskEvidence('doc "rulings"', evidence, { hrefFor: () => "" }),
    ).toBe('doc "rulings"');
  });

  it("does not rewrite code, existing links or fenced blocks", () => {
    const body = [
      '`doc "rulings"` stays code.',
      '[doc "rulings"](/elsewhere) stays linked.',
      "```",
      'doc "rulings" and PR #123',
      "```",
      "`PR #123` stays code.",
    ].join("\n");
    expect(linkifyAskEvidence(body, evidence, { hrefFor })).toBe(body);
  });

  it("returns the body unchanged when there is no evidence", () => {
    expect(linkifyAskEvidence("plain", { refs: [], links: [] })).toBe("plain");
    expect(linkifyAskEvidence("", evidence, { hrefFor })).toBe("");
  });
});

describe("stale asks and context requests in the Inbox list", () => {
  const openAsk = (overrides = {}) => ({
    id: "inbox-1",
    kind: "ask",
    title: "Pick a rollout",
    subject_ref: "card:anx-7",
    thread_id: "t-1",
    related_refs: ["thread:t-1"],
    response_proposals: ["Proceed"],
    ...overrides,
  });

  /*
   * `is_stale` is contract-shaped but core computes it per ask and does not put
   * it on an inbox row today, so these assert the fold for a core that does —
   * forward compatibility, not current server behaviour. See the issue.
   */
  it("marks an ask core called stale, and only while it is open", () => {
    expect(askIsStale({ is_stale: true })).toBe(true);
    expect(askIsStale({})).toBe(false);
    const rows = buildInboxRows({
      inboxItems: [
        openAsk({ is_stale: true }),
        openAsk({ id: "inbox-2", is_stale: false }),
        openAsk({
          id: "inbox-3",
          is_stale: true,
          status: "completed",
          responded_at: "2026-10-08T00:00:00.000Z",
          outcome: "answered",
        }),
      ],
      now: NOW,
    });
    const byId = new Map(rows.map((row) => [row.item.id, row]));
    expect(byId.get("inbox-1").stale).toBe(true);
    expect(byId.get("inbox-2").stale).toBe(false);
    // An answered ask is Handled; folding it under Stale would hide history.
    expect(byId.get("inbox-3").stale).toBe(false);
  });

  it("folds a stale ask into the Stale group with the stale tasks", () => {
    const rows = buildInboxRows({
      inboxItems: [openAsk({ is_stale: true }), openAsk({ id: "inbox-2" })],
      now: NOW,
    });
    const order = createInboxOrder();
    const grouped = order("needs-you:", rows, false);
    expect(grouped.currentRows.map((row) => row.item.id)).toEqual(["inbox-2"]);
    expect(grouped.staleRows.map((row) => row.item.id)).toEqual(["inbox-1"]);
  });

  it("badges a handled ask that was sent back for context", () => {
    expect(
      inboxRowBadge({ kind: "inbox", item: { outcome: "needs_context" } }, NOW),
    ).toEqual({ label: "Sent back for context", tone: "neutral" });
    expect(
      inboxRowBadge({ kind: "inbox", item: { outcome: "answered" } }, NOW),
    ).toBeNull();
  });
});

describe("the resolved subject is not repeated as evidence", () => {
  it("excludes the ref the context strip resolved to, not only subject_ref", () => {
    const model = askEvidenceModel({
      item: {
        subject_ref: "thread:t-1",
        related_refs: ["card:anx-7", "document:rulings"],
      },
      subjectRef: "card:anx-7",
    });
    expect(model.refs.map((entry) => entry.ref)).toEqual(["document:rulings"]);
  });
});

describe("a link destination cannot carry the rest of the line", () => {
  const evidence = {
    refs: [],
    links: [
      {
        label: "Change",
        // A parenthesis in the URL would end the markdown link early and hand
        // whoever wrote it the remaining text as markdown.
        url: "https://example.org/repo/pull/7)[spoof](https://evil.example/x",
        host: "example.org",
        kind: "Pull request",
      },
    ],
  };

  it("percent-encodes parentheses, angle brackets and spaces", () => {
    const out = linkifyAskEvidence("Ship PR #7 today.", evidence, {});
    // The whole URL stays inside one destination: brackets are legal there,
    // the parentheses are encoded, so nothing escapes into the body.
    expect(out).toBe(
      "Ship [PR #7](https://example.org/repo/pull/7%29[spoof]%28https://evil.example/x) today.",
    );
    expect(out).not.toContain("[spoof](");
  });

  it("escapes brackets in a linked name so the link cannot end early", () => {
    const refs = {
      refs: [{ ref: "document:a]b", prefix: "document", id: "a]b" }],
      links: [],
    };
    expect(
      linkifyAskEvidence('See doc "a]b" now.', refs, {
        hrefFor: () => "/docs/x",
      }),
    ).toBe("See doc [a\\]b](/docs/x) now.");
  });
});
