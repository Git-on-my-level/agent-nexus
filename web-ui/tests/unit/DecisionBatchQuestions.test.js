// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import { get, readable, writable } from "svelte/store";

vi.mock("$app/stores", () => ({
  page: readable({
    url: new URL("http://localhost/o/acme/w/main/decide"),
    params: { organization: "acme", workspace: "main" },
  }),
}));
const queue = vi.hoisted(() => ({ committed: new Set() }));
vi.mock("$lib/inboxResponseQueue.js", async () => {
  const { writable: store } = await import("svelte/store");
  return {
    queueInboxResponse: vi.fn(),
    captureInboxResponseBinding: vi.fn(() => ({ send: vi.fn() })),
    editInboxResponse: vi.fn(),
    retryInboxResponse: vi.fn(),
    resendInboxResponse: vi.fn(),
    discardInboxResponse: vi.fn(),
    onInboxResponseCommitted: (listener) => {
      queue.committed.add(listener);
      return () => queue.committed.delete(listener);
    },
    inboxResponseFailures: store({}),
    inboxResponseOverlay: store({}),
    inboxUnsentResponses: store([]),
  };
});
vi.mock("$lib/mutationOutbox.js", () => ({
  enqueuePmDecisionAnswer: vi.fn(async () => ({})),
  enqueueCurationUngroup: vi.fn(async () => ({})),
  mutationOutbox: writable([]),
  mutationConfirmations: writable({}),
  editMutation: vi.fn(),
  retryMutation: vi.fn(),
}));
const brief = vi.hoisted(() => ({ refreshBrief: vi.fn(async () => {}) }));
vi.mock("$lib/briefStore.js", () => brief);
vi.mock("$lib/actions/durableDraft.js", () => ({
  durableDraft: () => ({ update() {}, destroy() {} }),
}));

import DecisionCard from "../../src/lib/components/decide/DecisionCard.svelte";
import {
  inboxResponseOverlay,
  queueInboxResponse,
} from "$lib/inboxResponseQueue.js";
import { normalizeBrief } from "../../src/lib/brief.js";
import { batchAnswers } from "../../src/lib/decideBatch.js";

const ITEM = "inbox:ask:t1:askbatch_1:ev1";
const member = (n, extra = {}) => ({
  ref: `inbox:ask:t1:ev${n}:ev${n}`,
  inbox_item_id: `inbox:ask:t1:ev${n}:ev${n}`,
  event_ref: `event:ev${n}`,
  title: `Policy question ${n}?`,
  proposals: ["Yes", "No", "Later"],
  position: n,
  recommendation: { action: "Yes", why: "Matches the plan." },
  ...extra,
});
// Five questions, one shared context, question 3 multi-select.
const raw = {
  ref: ITEM,
  inbox_item_id: ITEM,
  kind: "ask",
  title: "Log retention interview",
  proposals: ["Yes", "No", "Later"],
  recommendation: { action: "Yes", why: "Lead member copy." },
  requested_by: { id: "a1", display: "Retention agent", kind: "agent" },
  batch: {
    key: "filed:askbatch_1",
    count: 5,
    context: {
      situation: "Every log is kept for 400 days.",
      impact: "Audits need a written policy.",
      details: "From the storage review.",
      evidence: [{ label: "Review", url: "https://example.com/review" }],
    },
    members: [
      member(1),
      member(2),
      member(3, { multi_select: true, note: "Pick every team." }),
      member(4),
      member(5),
    ],
  },
};
const decide = (decision) =>
  normalizeBrief({ decisions: [decision] }).decisions[0];
const click = (container, selector) =>
  fireEvent.click(container.querySelector(selector));

afterEach(() => {
  cleanup();
  queueInboxResponse.mockClear();
  brief.refreshBrief.mockClear();
  inboxResponseOverlay.set({});
});

describe("filed batch as numbered questions", () => {
  // Acceptance (oss#538): the web UI shows the batch as one item with the
  // context stated once, then numbered questions with the recommendation
  // marked.
  it("states the shared context once above numbered questions", () => {
    const { container } = render(DecisionCard, {
      decision: decide(raw),
      variant: "pane",
    });
    expect(container.querySelectorAll("[data-decision-batch]")).toHaveLength(1);
    const text = container.textContent;
    expect(text.split("Every log is kept for 400 days.")).toHaveLength(2);
    expect(text).not.toContain("Lead member copy.");
    const questions = [
      ...container.querySelectorAll("[data-decision-batch-question]"),
    ];
    expect(questions.map((q) => q.dataset.decisionBatchQuestion)).toEqual([
      "1",
      "2",
      "3",
      "4",
      "5",
    ]);
    expect(questions[2].textContent).toContain("(pick any)");
    expect(questions[2].textContent).toContain("Pick every team.");
    expect(
      container.querySelector("[data-decision-batch-option='1a']").dataset
        .recommended,
    ).toBe("true");
    // The card's flat proposals and reply composer are not offered.
    expect(container.querySelector("[data-decision-proposal]")).toBeNull();
    expect(
      container.querySelector("[data-decision-reply]").closest("form")
        .classList,
    ).toContain("hidden");
  });

  it("submits every answer in one request", async () => {
    const onAnswered = vi.fn();
    const { container } = render(DecisionCard, {
      decision: decide(raw),
      variant: "pane",
      onAnswered,
    });
    await click(container, "[data-decision-batch-option='1a']");
    await click(container, "[data-decision-batch-option='2b']");
    await click(container, "[data-decision-batch-option='3a']");
    await click(container, "[data-decision-batch-option='3c']");
    const other = container.querySelector("[data-decision-batch-other='4']");
    await fireEvent.input(other, { target: { value: "free text" } });
    await click(container, "[data-decision-batch-option='5a']");
    await click(container, "[data-decision-batch-submit]");
    expect(queueInboxResponse).toHaveBeenCalledTimes(1);
    const call = queueInboxResponse.mock.calls[0][0];
    expect(call.itemId).toBe(ITEM);
    expect(call.request).toEqual({
      answers: [
        { ask_ref: "event:ev1", response_text: "Yes", outcome: "answered" },
        { ask_ref: "event:ev2", response_text: "No", outcome: "answered" },
        {
          ask_ref: "event:ev3",
          response_text: "Yes; Later",
          outcome: "answered",
        },
        {
          ask_ref: "event:ev4",
          response_text: "free text",
          outcome: "answered",
        },
        { ask_ref: "event:ev5", response_text: "Yes", outcome: "answered" },
      ],
    });
    expect(onAnswered).toHaveBeenCalledTimes(1);
  });

  // Acceptance (oss#538): partial submit records answered questions and
  // leaves the rest open.
  it("sends a partial submit and reloads the questions still open", async () => {
    const onAnswered = vi.fn();
    const { container } = render(DecisionCard, {
      decision: decide(raw),
      variant: "pane",
      onAnswered,
    });
    await click(container, "[data-decision-batch-option='2a']");
    await click(container, "[data-decision-batch-option='2b']");
    expect(
      container.querySelector("[data-decision-batch-submit]").textContent,
    ).toContain("Submit 1 of 5");
    await click(container, "[data-decision-batch-submit]");
    const call = queueInboxResponse.mock.calls[0][0];
    expect(call.request).toEqual({
      partial: true,
      answers: [
        { ask_ref: "event:ev2", response_text: "No", outcome: "answered" },
      ],
    });
    expect(onAnswered).not.toHaveBeenCalled();
    inboxResponseOverlay.set({ [ITEM]: { status: "committed" } });
    for (const listener of queue.committed)
      listener(ITEM, { open_ask_count: 4 });
    expect(get(inboxResponseOverlay)[ITEM]).toBeUndefined();
    expect(brief.refreshBrief).toHaveBeenCalled();
  });

  it("keeps the grouped view for payloads without per-question proposals", () => {
    const legacy = {
      ...raw,
      batch: {
        ...raw.batch,
        members: raw.batch.members.map((m) => ({ ...m, proposals: undefined })),
      },
    };
    const { container } = render(DecisionCard, { decision: decide(legacy) });
    expect(
      container.querySelector("[data-decision-batch-questions]"),
    ).toBeNull();
    expect(container.textContent).toContain("5 grouped asks");
  });
});

describe("batchAnswers", () => {
  const members = decide(raw).batch.members;
  it("returns null until something is answered", () => {
    expect(batchAnswers(members, {}, {})).toBeNull();
  });
  it("prefers own words over a choice and orders choices", () => {
    expect(
      batchAnswers(
        members,
        { "event:ev3": [2, 0], "event:ev1": [1] },
        { "event:ev1": " Only for finance " },
      ),
    ).toEqual({
      partial: true,
      answers: [
        {
          ask_ref: "event:ev1",
          response_text: "Only for finance",
          outcome: "answered",
        },
        {
          ask_ref: "event:ev3",
          response_text: "Yes; Later",
          outcome: "answered",
        },
      ],
    });
  });
});
