// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const core = vi.hoisted(() => ({ rulingsList: vi.fn() }));
vi.mock("$lib/coreClient", () => ({ coreClient: core }));
const outbox = await vi.hoisted(async () => {
  const { writable } = await import("svelte/store");
  return {
    enqueueLocalMutation: vi.fn(async (entry) => entry),
    mutationOutbox: writable([]),
    mutationConfirmations: writable({}),
    editMutation: vi.fn(),
    retryMutation: vi.fn(),
  };
});
vi.mock("$lib/mutationOutbox.js", () => outbox);
vi.mock("$lib/inboxResponseQueue.js", () => ({
  onInboxResponseCommitted: vi.fn(() => () => {}),
}));
vi.mock("$lib/briefStore.js", () => ({ refreshBrief: vi.fn(async () => {}) }));

import StandingRulings from "../../src/lib/components/decide/StandingRulings.svelte";
import { revokeTarget } from "../../src/lib/standingRulings.js";

const rulings = [
  {
    id: "r1",
    ref: "ruling:r1",
    kind: "answer",
    question: "Rerun the counts-only diagnostic?",
    answer: "Yes, counts-only reruns never need me.",
    created_by: "human:operator",
    created_at: "2026-10-12T08:00:00Z",
    uses: 4,
    active: true,
  },
  {
    id: "r2",
    ref: "ruling:r2",
    kind: "not_a_decision",
    question: "Daily digest 2026-10-12: change any priorities?",
    answer: "",
    requester: "guard",
    created_by: "human:operator",
    created_at: "2026-10-13T08:00:00Z",
    uses: 1,
    active: true,
  },
];
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
const toggle = (c) => c.querySelector("[data-standing-rulings-toggle]");

beforeEach(() => {
  core.rulingsList.mockReset().mockResolvedValue({ rulings });
  outbox.enqueueLocalMutation.mockReset().mockImplementation(async (e) => e);
  outbox.mutationOutbox.set([]);
  outbox.mutationConfirmations.set({});
});
afterEach(cleanup);

describe("StandingRulings", () => {
  it("stays collapsed and reads nothing until opened", async () => {
    const { container } = render(StandingRulings);
    expect(text(toggle(container))).toBe("Standing rulings Show");
    expect(core.rulingsList).not.toHaveBeenCalled();
    await fireEvent.click(toggle(container));
    await vi.waitFor(() =>
      expect(container.querySelectorAll("[data-standing-ruling]")).toHaveLength(
        2,
      ),
    );
    expect(core.rulingsList).toHaveBeenCalledTimes(1);
    expect(text(toggle(container))).toBe("Standing rulings · 2 Hide");
    expect(text(container.querySelector('[data-standing-ruling="r1"]'))).toBe(
      "Rerun the counts-only diagnostic? → Yes, counts-only reruns never need me. used 4 times · made Oct 12 Revoke",
    );
    expect(text(container.querySelector('[data-standing-ruling="r2"]'))).toBe(
      "Daily digest 2026-10-12: change any priorities? Not a decision · from guard used 1 time · made Oct 13 Revoke",
    );
  });

  it("revokes through the outbox and drops the row", async () => {
    const { container } = render(StandingRulings);
    await fireEvent.click(toggle(container));
    await vi.waitFor(() =>
      expect(container.querySelector('[data-standing-ruling="r1"]')).not.toBe(
        null,
      ),
    );
    await fireEvent.click(
      container.querySelector(
        '[data-standing-ruling="r1"] [data-standing-ruling-revoke]',
      ),
    );
    await vi.waitFor(() =>
      expect(outbox.enqueueLocalMutation).toHaveBeenCalledTimes(1),
    );
    expect(outbox.enqueueLocalMutation.mock.calls[0][0]).toMatchObject({
      target: revokeTarget("r1"),
      operation: "rulings.revoke",
      request: { id: "r1" },
    });
    // The saved revoke (pending, then confirmed) takes the row out.
    outbox.mutationOutbox.set([
      { key: "k", target: revokeTarget("r1"), state: "pending" },
    ]);
    await vi.waitFor(() =>
      expect(container.querySelector('[data-standing-ruling="r1"]')).toBeNull(),
    );
    outbox.mutationOutbox.set([]);
    outbox.mutationConfirmations.set({ [revokeTarget("r1")]: {} });
    await vi.waitFor(() =>
      expect(text(toggle(container))).toBe("Standing rulings · 1 Hide"),
    );
  });

  it("explains how a ruling is made when there are none", async () => {
    core.rulingsList.mockResolvedValue({ rulings: [] });
    const { container } = render(StandingRulings);
    await fireEvent.click(toggle(container));
    await vi.waitFor(() =>
      expect(
        container.querySelector("[data-standing-rulings-empty]"),
      ).not.toBeNull(),
    );
    expect(text(container.querySelector("[data-standing-rulings-empty]"))).toBe(
      "None yet. An answer becomes a ruling when you tick “Always answer this way” before answering.",
    );
  });

  it("offers a retry when the list cannot be read", async () => {
    core.rulingsList.mockRejectedValueOnce(new Error("boom: raw upstream"));
    const { container, getByRole } = render(StandingRulings);
    await fireEvent.click(toggle(container));
    await vi.waitFor(() =>
      expect(container.querySelector('[role="alert"]')).not.toBeNull(),
    );
    expect(text(container.querySelector('[role="alert"]'))).toBe(
      "Couldn’t load standing rulings. Retry",
    );
    await fireEvent.click(getByRole("button", { name: "Retry" }));
    await vi.waitFor(() =>
      expect(container.querySelectorAll("[data-standing-ruling]")).toHaveLength(
        2,
      ),
    );
  });

  it("surfaces a ruling that failed to save, without hiding the list", () => {
    outbox.mutationOutbox.set([
      {
        key: "k9",
        target: "ruling:event:ask-91",
        operation: "rulings.create",
        state: "attention",
        text: "Merge? → Merge",
        request: {},
      },
    ]);
    const { container } = render(StandingRulings);
    expect(
      container.querySelector("[data-mutation-state]").dataset.mutationState,
    ).toBe("attention");
    expect(toggle(container)).not.toBeNull();
  });
});
