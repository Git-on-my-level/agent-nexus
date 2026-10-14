// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readable, writable } from "svelte/store";

vi.mock("$app/stores", () => ({
  page: readable({
    url: new URL("http://localhost/o/acme/w/main/decide"),
    params: { organization: "acme", workspace: "main" },
  }),
}));

const queue = vi.hoisted(() => ({
  queueInboxResponse: vi.fn(() => 1),
  captureInboxResponseBinding: vi.fn(() => ({ scope: "s", identity: "i" })),
}));
vi.mock("$lib/inboxResponseQueue.js", () => ({
  ...queue,
  editInboxResponse: vi.fn(),
  retryInboxResponse: vi.fn(),
  inboxResponseFailures: writable({}),
  inboxResponseOverlay: writable({}),
  inboxUnsentResponses: writable([]),
}));
vi.mock("$lib/actions/durableDraft.js", () => ({
  durableDraft: () => ({ update() {}, destroy() {} }),
}));
const clipboard = vi.hoisted(() => ({ copyText: vi.fn(async () => true) }));
vi.mock("$lib/clipboard.js", () => clipboard);

import DecisionCard from "../../src/lib/components/decide/DecisionCard.svelte";
import { normalizeBrief, sortDecisions } from "../../src/lib/brief.js";
import { lintComputed } from "./briefLint.js";
import fixture from "../../../contracts/fixtures/brief/brief.v1.json";

const brief = normalizeBrief(fixture);
const NOW = Date.parse("2026-10-15T09:00:00Z");
const merge = brief.decisions.find((d) => d.ref === "inbox:ask-91");

beforeEach(() => {
  queue.queueInboxResponse.mockClear();
  clipboard.copyText.mockClear();
});
afterEach(cleanup);

describe("DecisionCard", () => {
  it("puts the recommended proposal first and answers in one tap", async () => {
    const { container } = render(DecisionCard, { decision: merge, now: NOW });
    const buttons = container.querySelectorAll("[data-decision-proposal]");
    expect(buttons[0].textContent).toContain("Merge");
    expect(buttons[0].dataset.recommended).toBe("true");
    expect(buttons[0].textContent).toContain("(recommended)");
    await fireEvent.click(buttons[0]);
    expect(queue.queueInboxResponse).toHaveBeenCalledTimes(1);
    const call = queue.queueInboxResponse.mock.calls[0][0];
    expect(call.itemId).toBe("ask-91");
    expect(call.request).toMatchObject({
      response_text: "Merge",
      outcome: "answered",
    });
  });

  it("shows blocked titles, not slugs, and the deadline as computed", () => {
    const { container } = render(DecisionCard, { decision: merge, now: NOW });
    const blocks = container.querySelector(
      '[data-source="computed:decisions.blocks"]',
    );
    expect(blocks.textContent).toContain("Battery curve v2");
    expect(blocks.textContent).toContain("blocks 2");
    expect(
      container.querySelector('[data-source="computed:decisions.decide_by"]')
        .textContent,
    ).toBe("by Oct 17");
  });

  it("answers access and host decisions with approved or rejected", async () => {
    const host = brief.decisions.find((d) => d.kind === "host");
    const { container } = render(DecisionCard, { decision: host, now: NOW });
    expect(container.querySelector("[data-decision-reply-toggle]")).toBeNull();
    const deny = container.querySelectorAll("[data-decision-proposal]")[1];
    await fireEvent.click(deny);
    expect(queue.queueInboxResponse.mock.calls[0][0].request.outcome).toBe(
      "rejected",
    );
  });

  it("copies the ref from the button and from the c key", async () => {
    const { container } = render(DecisionCard, { decision: merge, now: NOW });
    await fireEvent.click(container.querySelector("[data-decision-copy-ref]"));
    expect(clipboard.copyText).toHaveBeenLastCalledWith("inbox:ask-91");
    const row = container.querySelector("[data-decision-ref]");
    await fireEvent.keyDown(row, { key: "c" });
    expect(clipboard.copyText).toHaveBeenCalledTimes(2);
  });

  it("links the row to the permalink and the full variant shows no link", () => {
    const row = render(DecisionCard, { decision: merge, now: NOW });
    expect(
      row.container.querySelector("[data-decision-link]").getAttribute("href"),
    ).toBe("/o/acme/w/main/decide/ask-91");
    cleanup();
    const full = render(DecisionCard, {
      decision: merge,
      now: NOW,
      variant: "full",
    });
    expect(full.container.querySelector("[data-decision-link]")).toBeNull();
  });

  it("passes the I1 computed-source lint for every fixture decision", () => {
    for (const decision of sortDecisions(brief.decisions)) {
      const { container } = render(DecisionCard, { decision, now: NOW });
      expect(lintComputed(container), decision.ref).toEqual([]);
      cleanup();
    }
  });
});
