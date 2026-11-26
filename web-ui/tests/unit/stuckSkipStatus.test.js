// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, expect, it, vi } from "vitest";

/**
 * Stuck skips (2026-11-26): an older open tab marked a reader's bulk skips
 * "Unknown saved operation". The unsent list then filled most of the screen
 * above Decide, and each row offered a read-only "Saved message" with
 * "Edit message" although a skip has no text the reader wrote.
 */
const h = vi.hoisted(() => ({ retried: [], discarded: [] }));
vi.mock("$lib/mutationOutbox.js", async () => {
  const { writable } = await import("svelte/store");
  return {
    mutationOutbox: writable([]),
    mutationConfirmations: writable({}),
    retryMutation: vi.fn(async (key) => h.retried.push(key)),
    discardMutation: vi.fn(async (key) => h.discarded.push(key)),
    editMutation: vi.fn(),
  };
});

import UnsentIndicator from "../../src/lib/components/inbox/UnsentIndicator.svelte";
import MutationSendStatus from "../../src/lib/components/MutationSendStatus.svelte";
import { mutationOutbox } from "$lib/mutationOutbox.js";
import { UNKNOWN_OPERATION } from "../../src/lib/outboxOperations.js";

const stuck = (key, operation = "inbox.skip_many") => ({
  key,
  target: `skip-many:${key}`,
  operation,
  title: "Skip 3 decisions",
  text: "Who may publish?",
  state: "attention",
  failure: {
    message: "This response needs a correction before it can be sent.",
    detail: UNKNOWN_OPERATION,
  },
  request: { items: [] },
});

afterEach(() => {
  cleanup();
  mutationOutbox.set([]);
  h.retried.length = 0;
  h.discarded.length = 0;
});

it("offers retry or keep for a stuck skip, never a saved message to edit", async () => {
  render(MutationSendStatus, { entry: stuck("a") });
  expect(document.querySelector("textarea")).toBeNull();
  expect(document.body.textContent).not.toContain("Edit message");
  expect(document.body.textContent).toContain("older open tab");
  const buttons = [...document.querySelectorAll("button")];
  await fireEvent.click(buttons.find((b) => b.textContent === "Retry skip"));
  await fireEvent.click(buttons.find((b) => b.textContent === "Don't skip"));
  expect(h.retried).toEqual(["a"]);
  expect(h.discarded).toEqual(["a"]);
});

it("says so when discarding a stuck skip fails on this device", async () => {
  const { discardMutation } = await import("$lib/mutationOutbox.js");
  discardMutation.mockRejectedValueOnce(new Error("storage unavailable"));
  render(MutationSendStatus, { entry: stuck("d") });
  const keep = [...document.querySelectorAll("button")].find(
    (b) => b.textContent === "Don't skip",
  );
  await fireEvent.click(keep);
  await new Promise((resolve) => setTimeout(resolve));
  expect(document.querySelector('[role="alert"]').textContent).toContain(
    "Couldn’t discard",
  );
});

it("keeps the saved message for work the reader wrote", () => {
  render(MutationSendStatus, {
    entry: { ...stuck("c", "events.create"), text: "My comment" },
  });
  expect(document.querySelector("textarea").value).toBe("My comment");
});

it("floats the open list over the page and retries every stuck row at once", async () => {
  mutationOutbox.set([stuck("a"), stuck("b", "inbox.skip")]);
  render(UnsentIndicator);
  const details = document.querySelector("[data-unsent-indicator]");
  details.open = true;
  await fireEvent(details, new Event("toggle", { bubbles: true }));
  const list = document.querySelector("[data-unsent-list]");
  expect(list.className).toContain("absolute");
  await fireEvent.click(document.querySelector("[data-unsent-retry-all]"));
  expect(h.retried).toEqual(["a", "b"]);
});
