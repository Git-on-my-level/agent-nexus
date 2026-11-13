// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { readable, writable } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("$app/stores", () => ({
  page: readable({
    url: new URL("http://localhost/o/acme/w/main/decide"),
    params: { organization: "acme", workspace: "main" },
  }),
}));
const queue = await vi.hoisted(async () => {
  const { writable } = await import("svelte/store");
  return {
    queueInboxResponse: vi.fn(() => 1),
    captureInboxResponseBinding: vi.fn(() => ({ scope: "s", identity: "i" })),
    listeners: [],
    failures: writable({}),
    rows: writable([]),
  };
});
vi.mock("$lib/inboxResponseQueue.js", () => ({
  queueInboxResponse: queue.queueInboxResponse,
  captureInboxResponseBinding: queue.captureInboxResponseBinding,
  onInboxResponseCommitted: (listener) => {
    queue.listeners.push(listener);
    return () => {};
  },
  resendInboxResponse: vi.fn(),
  retryInboxResponse: vi.fn(),
  editInboxResponse: vi.fn(),
  inboxResponseFailures: queue.failures,
  inboxResponseOverlay: writable({}),
  inboxUnsentResponses: writable([]),
}));
const outbox = vi.hoisted(() => ({
  enqueueLocalMutation: vi.fn(async (entry) => entry),
  enqueuePmDecisionAnswer: vi.fn(async () => ({})),
  enqueueCurationUngroup: vi.fn(async () => ({})),
}));
vi.mock("$lib/mutationOutbox.js", () => ({
  ...outbox,
  mutationOutbox: queue.rows,
  mutationConfirmations: writable({}),
  editMutation: vi.fn(),
  retryMutation: vi.fn(),
}));
vi.mock("$lib/briefStore.js", () => ({ refreshBrief: vi.fn(async () => {}) }));
vi.mock("$lib/actions/durableDraft.js", () => ({
  durableDraft: () => ({ update() {}, destroy() {} }),
}));
vi.mock("$lib/clipboard.js", () => ({ copyText: vi.fn(async () => true) }));

import DecisionCard from "../../src/lib/components/decide/DecisionCard.svelte";
import StakesBadges from "../../src/lib/components/decide/StakesBadges.svelte";
import { normalizeBrief } from "../../src/lib/brief.js";
import {
  NOT_A_DECISION_REASON,
  notADecisionTarget,
  resetStandingAnswers,
} from "../../src/lib/standingRulings.js";
import { lintComputed } from "./briefLint.js";
import fixture from "../../../contracts/fixtures/brief/brief.v1.json";

const NOW = Date.parse("2026-10-15T09:00:00Z");
const brief = normalizeBrief(fixture);
const merge = brief.decisions.find((d) => d.ref === "inbox:ask-91");
const access = brief.decisions.find((d) => d.kind === "access");
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();

beforeEach(() => {
  resetStandingAnswers();
  queue.listeners.length = 0;
  queue.queueInboxResponse.mockClear();
  queue.rows.set([]);
  outbox.enqueueLocalMutation.mockReset().mockImplementation(async (e) => e);
});
afterEach(cleanup);

describe("stakes badges", () => {
  it("shows what the asker says is at risk, tagged as computed", () => {
    const { container } = render(DecisionCard, { decision: merge, now: NOW });
    const irreversible = container.querySelector("[data-stakes-irreversible]");
    expect(irreversible.textContent).toBe("Irreversible");
    expect(irreversible.closest(".ui-badge").className).toContain(
      "ui-badge--warn",
    );
    expect(container.querySelector("[data-stakes-cost]").textContent).toBe(
      "Cost: $1,200 of rig time",
    );
    expect(lintComputed(container)).toEqual([]);
  });

  it("renders nothing without stakes, and each badge on its own", () => {
    expect(
      render(StakesBadges, { stakes: null }).container.textContent.trim(),
    ).toBe("");
    cleanup();
    const { container } = render(StakesBadges, {
      stakes: { irreversible: false, cost: "2 days" },
    });
    expect(container.querySelector("[data-stakes-irreversible]")).toBeNull();
    expect(text(container)).toBe("Cost: 2 days");
  });
});

describe("Not a decision", () => {
  it("sends the ask back through the outbox and tells the list", async () => {
    const onAnswered = vi.fn();
    const { container } = render(DecisionCard, {
      decision: merge,
      now: NOW,
      onAnswered,
    });
    await fireEvent.click(container.querySelector("[data-not-a-decision]"));
    const confirm = container.querySelector("[data-not-a-decision-confirm]");
    expect(text(confirm)).toContain(
      "Always, for this kind of ask from build-agent on host-1",
    );
    // Nothing is sent until the reader confirms.
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
    await fireEvent.click(
      container.querySelector("[data-not-a-decision-always]"),
    );
    await fireEvent.click(
      container.querySelector("[data-not-a-decision-send]"),
    );
    await vi.waitFor(() =>
      expect(outbox.enqueueLocalMutation).toHaveBeenCalledTimes(1),
    );
    expect(outbox.enqueueLocalMutation.mock.calls[0][0]).toMatchObject({
      target: notADecisionTarget(merge.inboxItemId),
      operation: "asks.not_a_decision",
      request: {
        ask_ref: "event:ask-91",
        always: true,
        body: { reason: NOT_A_DECISION_REASON, evidence: ["event:ask-91"] },
      },
    });
    await vi.waitFor(() => expect(onAnswered).toHaveBeenCalledWith(merge));
    expect(text(container)).toContain("Sent back as not a decision.");
    // An answer was never queued: sending back is not answering.
    expect(queue.queueInboxResponse).not.toHaveBeenCalled();
  });

  it("keeps the confirm open and says so when it cannot be saved", async () => {
    outbox.enqueueLocalMutation.mockRejectedValueOnce(new Error("quota"));
    const onAnswered = vi.fn();
    const { container } = render(DecisionCard, {
      decision: merge,
      now: NOW,
      onAnswered,
    });
    await fireEvent.click(container.querySelector("[data-not-a-decision]"));
    await fireEvent.click(
      container.querySelector("[data-not-a-decision-send]"),
    );
    await vi.waitFor(() =>
      expect(container.querySelector('[role="alert"]')).not.toBeNull(),
    );
    expect(text(container.querySelector('[role="alert"]'))).toBe(
      "Couldn’t save this on this device, so the ask was not sent back.",
    );
    expect(onAnswered).not.toHaveBeenCalled();
    expect(
      container.querySelector("[data-not-a-decision-confirm]"),
    ).not.toBeNull();
  });

  it("shows the saved request with its retry while it waits or fails", () => {
    queue.rows.set([
      {
        key: "k1",
        target: notADecisionTarget(merge.inboxItemId),
        operation: "asks.not_a_decision",
        state: "attention",
        text: merge.title,
        request: {},
      },
    ]);
    const { container } = render(DecisionCard, { decision: merge, now: NOW });
    const status = container.querySelector("[data-not-a-decision-status]");
    expect(
      status.querySelector("[data-mutation-state]").dataset.mutationState,
    ).toBe("attention");
    expect(container.querySelector("[data-not-a-decision]")).toBeNull();
  });

  it("is not offered where a decision has its own answer path", () => {
    for (const decision of [
      access,
      { ...merge, eventRef: "" },
      { ...merge, kind: "pm_proposal" },
    ]) {
      const { container } = render(DecisionCard, { decision, now: NOW });
      expect(container.querySelector("[data-not-a-decision]")).toBeNull();
      expect(container.querySelector("[data-decision-always]")).toBeNull();
      cleanup();
    }
    // Answering for another workspace: the outbox would send to this one.
    const { container } = render(DecisionCard, {
      decision: merge,
      now: NOW,
      binding: { send: vi.fn(), scope: "s", identity: "i" },
    });
    expect(container.querySelector("[data-not-a-decision]")).toBeNull();
  });
});

describe("Always answer this way", () => {
  it("rules only once the ticked answer is committed", async () => {
    const { container } = render(DecisionCard, { decision: merge, now: NOW });
    await fireEvent.click(container.querySelector("[data-decision-always]"));
    await fireEvent.click(
      container.querySelector('[data-decision-proposal="1"]'),
    );
    expect(queue.queueInboxResponse).toHaveBeenCalledTimes(1);
    // The answer waits behind undo; no ruling yet.
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
    queue.listeners[0](merge.inboxItemId, {});
    await vi.waitFor(() =>
      expect(outbox.enqueueLocalMutation).toHaveBeenCalledTimes(1),
    );
    expect(outbox.enqueueLocalMutation.mock.calls[0][0]).toMatchObject({
      operation: "rulings.create",
      request: {
        body: { ask_ref: "event:ask-91", kind: "answer", answer: "Merge" },
      },
    });
  });

  it("makes no ruling for an answer sent without the box", async () => {
    const { container } = render(DecisionCard, { decision: merge, now: NOW });
    await fireEvent.click(
      container.querySelector('[data-decision-proposal="1"]'),
    );
    queue.listeners[0](merge.inboxItemId, {});
    await Promise.resolve();
    expect(outbox.enqueueLocalMutation).not.toHaveBeenCalled();
  });
});
