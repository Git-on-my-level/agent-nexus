// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it } from "vitest";

import UnsentIndicator from "../../src/lib/components/inbox/UnsentIndicator.svelte";
import { mutationOutbox } from "../../src/lib/mutationOutbox.js";
import { inboxUnsentResponses } from "../../src/lib/inboxResponseQueue.js";

afterEach(() => {
  cleanup();
  inboxUnsentResponses.set([]);
  mutationOutbox.set([]);
});

it("does not lay out send status while the unsent chip is closed", () => {
  inboxUnsentResponses.set([
    {
      itemId: "inbox:a",
      item: { title: "Need a decision" },
      request: { idempotency_key: "k1", response_text: "Ship it" },
      state: "pending",
    },
  ]);
  render(UnsentIndicator);
  expect(document.querySelector("[data-unsent-indicator]")).toBeTruthy();
  expect(document.querySelector("[data-inbox-send-state]")).toBeNull();
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
