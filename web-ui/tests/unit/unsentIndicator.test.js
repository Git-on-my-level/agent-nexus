// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it } from "vitest";

import UnsentIndicator from "../../src/lib/components/inbox/UnsentIndicator.svelte";
import { inboxUnsentResponses } from "../../src/lib/inboxResponseQueue.js";

afterEach(() => {
  cleanup();
  inboxUnsentResponses.set([]);
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
