// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import { readable, writable } from "svelte/store";

vi.mock("$app/stores", () => ({
  page: readable({
    url: new URL("http://localhost/o/acme/w/main/decide"),
    params: { organization: "acme", workspace: "main" },
  }),
}));
vi.mock("$lib/inboxResponseQueue.js", () => ({
  queueInboxResponse: vi.fn(),
  captureInboxResponseBinding: vi.fn(),
  editInboxResponse: vi.fn(),
  retryInboxResponse: vi.fn(),
  inboxResponseFailures: writable({}),
  inboxResponseOverlay: writable({}),
  inboxUnsentResponses: writable([]),
}));
const outbox = vi.hoisted(() => ({
  enqueuePmDecisionAnswer: vi.fn(async () => ({})),
  enqueueCurationUngroup: vi.fn(async () => ({})),
}));
vi.mock("$lib/mutationOutbox.js", () => ({
  ...outbox,
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
import { normalizeBrief } from "../../src/lib/brief.js";

const raw = {
  ref: "inbox:ask-1",
  kind: "ask",
  inbox_item_id: "ask-1",
  title: "Archive stale asks",
  batch: {
    key: "batch-key-1",
    count: 3,
    members: [
      { ref: "inbox:ask-1", inbox_item_id: "ask-1", title: "Old deploy ask" },
      { ref: "inbox:ask-2", inbox_item_id: "ask-2", title: "" },
      { ref: "inbox:ask-3", inbox_item_id: "ask-3", title: "Stale review" },
    ],
  },
};
const decide = (decision) =>
  normalizeBrief({ decisions: [decision] }).decisions[0];

afterEach(() => {
  cleanup();
  outbox.enqueueCurationUngroup.mockClear();
});

describe("DecisionCard batch", () => {
  it("normalizes a missing batch to null", () => {
    expect(decide({ ...raw, batch: undefined }).batch).toBeNull();
  });

  it("shows the exact count, members and an Ungroup button", () => {
    const { container } = render(DecisionCard, { decision: decide(raw) });
    const batch = container.querySelector("[data-decision-batch]");
    expect(batch.textContent).toContain("3 grouped asks");
    const items = [...batch.querySelectorAll("li")].map((li) =>
      li.textContent.trim(),
    );
    expect(items).toEqual(["Old deploy ask", "inbox:ask-2", "Stale review"]);
    expect(
      container.querySelector("[data-decision-ungroup]").textContent,
    ).toContain("Ungroup");
  });

  it("queues the ungroup through the outbox with the batch key", async () => {
    const { container } = render(DecisionCard, { decision: decide(raw) });
    await fireEvent.click(container.querySelector("[data-decision-ungroup]"));
    expect(outbox.enqueueCurationUngroup).toHaveBeenCalledWith(
      expect.objectContaining({
        key: "batch-key-1",
        target: "curation-batch:batch-key-1",
      }),
    );
  });

  it("says so when the ungroup cannot be saved", async () => {
    outbox.enqueueCurationUngroup.mockRejectedValueOnce(new Error("quota"));
    const { container, findByRole } = render(DecisionCard, {
      decision: decide(raw),
    });
    await fireEvent.click(container.querySelector("[data-decision-ungroup]"));
    expect((await findByRole("alert")).textContent).toContain("not sent");
  });

  it("refreshes the brief once the ungroup is confirmed", async () => {
    render(DecisionCard, { decision: decide(raw) });
    window.dispatchEvent(
      new CustomEvent("anx:mutation-confirmed", {
        detail: { target: "curation-batch:batch-key-1" },
      }),
    );
    expect(brief.refreshBrief).toHaveBeenCalled();
  });
});
