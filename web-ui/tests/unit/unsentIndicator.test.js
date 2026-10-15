// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, expect, it, vi } from "vitest";

import UnsentIndicator from "../../src/lib/components/inbox/UnsentIndicator.svelte";
import { mutationOutbox } from "../../src/lib/mutationOutbox.js";
import { inboxUnsentResponses } from "../../src/lib/inboxResponseQueue.js";

afterEach(() => {
  vi.useRealTimers();
  cleanup();
  inboxUnsentResponses.set([]);
  mutationOutbox.set([]);
});

it("does not lay out send status while the unsent chip is closed", async () => {
  inboxUnsentResponses.set([
    {
      itemId: "inbox:a",
      item: { title: "Need a decision" },
      request: { idempotency_key: "k1", response_text: "Ship it" },
      state: "pending",
    },
  ]);
  vi.useFakeTimers();
  render(UnsentIndicator);
  // A send that goes out within a moment never shows the chip.
  expect(document.querySelector("[data-unsent-indicator]")).toBeNull();
  await vi.advanceTimersByTimeAsync(2_000);
  expect(document.querySelector("[data-unsent-indicator]")).toBeTruthy();
  expect(document.querySelector("[data-inbox-send-state]")).toBeNull();
});

it("links a saved response to Decide in that response's workspace", async () => {
  inboxUnsentResponses.set([
    {
      itemId: "inbox:need-a-decision",
      item: { title: "Need a decision" },
      identity: JSON.stringify(["acme", "ops", "agent-1", "actor-1"]),
      request: { idempotency_key: "k-decide", response_text: "Ship it" },
      state: "pending",
    },
  ]);
  vi.useFakeTimers();
  render(UnsentIndicator);
  await vi.advanceTimersByTimeAsync(2_000);
  const details = document.querySelector("[data-unsent-indicator]");
  details.open = true;
  await fireEvent(details, new Event("toggle", { bubbles: true }));
  expect(
    document.querySelector("[data-unsent-indicator] a").getAttribute("href"),
  ).toBe("/o/acme/w/ops/decide/inbox%3Aneed-a-decision");
});

it("counts saved discussion work without laying out a closed status card", () => {
  mutationOutbox.set([
    {
      key: "comment-key",
      target: "discussion:thread:composer",
      title: "Saved comment",
      state: "attention",
      text: "Keep this comment",
    },
  ]);
  render(UnsentIndicator);
  expect(
    document.querySelector("[data-unsent-indicator] summary").textContent,
  ).toContain("1 unsent");
  expect(document.querySelector("[data-mutation-state]")).toBeNull();
});

it("leaves an answer still inside its undo window to the undo toast", async () => {
  const { inboxResponseToast } =
    await import("../../src/lib/inboxResponseQueue.js");
  inboxResponseToast.set({ id: 1, itemId: "inbox:a", state: "pending" });
  inboxUnsentResponses.set([
    {
      itemId: "inbox:a",
      item: { title: "Need a decision" },
      request: { idempotency_key: "k1", response_text: "Ship it" },
      state: "pending",
    },
  ]);
  vi.useFakeTimers();
  render(UnsentIndicator);
  await vi.advanceTimersByTimeAsync(5_000);
  expect(document.querySelector("[data-unsent-indicator]")).toBeNull();
  inboxResponseToast.set(null);
});
