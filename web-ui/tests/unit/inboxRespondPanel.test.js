// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import InboxRespondPanel from "../../src/lib/components/inbox/InboxRespondPanel.svelte";
import { PROPOSAL_FLASH_MS } from "../../src/lib/inboxProposalChoice.js";

/**
 * Choosing a suggested response, at the panel's own level.
 *
 * One ask takes one response. A suggestion goes out after a short flash, and
 * everything else that answers has to wait for it: the response queue commits
 * whatever is still waiting the moment a second answer is enqueued, so a
 * second click inside the flash sends both — the first past its own undo
 * window, the second for core to reject as a duplicate.
 */
function panel(props = {}) {
  const onSend = vi.fn();
  const onAcknowledge = vi.fn();
  const result = render(InboxRespondPanel, {
    props: {
      kind: "review",
      itemKey: "inbox-a",
      proposals: ["Ship it", "Hold for review"],
      onSend,
      onAcknowledge,
      ...props,
    },
  });
  return { onSend, onAcknowledge, ...result };
}

const button = (label) => {
  const found = [...document.querySelectorAll("button")].find(
    (candidate) => candidate.textContent.trim() === label,
  );
  if (!found) throw new Error(`no button labelled ${label}`);
  return found;
};

const settle = () =>
  new Promise((resolve) => setTimeout(resolve, PROPOSAL_FLASH_MS + 60));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("answering while a suggestion is on its way out", () => {
  it("holds Approve and Reject until the chosen response has gone", async () => {
    const { onSend } = panel();

    await fireEvent.click(document.querySelector('[data-inbox-proposal="1"]'));
    expect(button("Approve").disabled).toBe(true);
    expect(button("Reject").disabled).toBe(true);
    await fireEvent.click(button("Approve"));
    await fireEvent.click(button("Reject"));
    expect(onSend).not.toHaveBeenCalled();

    await settle();
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenCalledWith("Ship it", "answered", "inbox-a");
    // And they are answerable again once nothing is pending.
    expect(button("Approve").disabled).toBe(false);
  });

  it("holds the reply and Acknowledge too", async () => {
    const { onSend, onAcknowledge } = panel({ draft: "a typed reply" });

    await fireEvent.click(document.querySelector('[data-inbox-proposal="2"]'));
    expect(button("Send reply").disabled).toBe(true);
    expect(button("Acknowledge").disabled).toBe(true);
    await fireEvent.click(button("Acknowledge"));
    expect(onAcknowledge).not.toHaveBeenCalled();

    await settle();
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenCalledWith(
      "Hold for review",
      "answered",
      "inbox-a",
    );
  });

  it("answers the item the reader chose on, not the one on screen later", async () => {
    // What `sendContext` is for: the caller captures the item and its
    // composer when the reader chooses, and gets it back with the send.
    let current = "inbox-a";
    const { onSend } = panel({ sendContext: () => current });

    await fireEvent.click(document.querySelector('[data-inbox-proposal="1"]'));
    current = "inbox-b";
    await settle();

    expect(onSend).toHaveBeenCalledWith("Ship it", "answered", "inbox-a");
  });

  it("sends a review decision straight away when nothing is pending", async () => {
    const { onSend } = panel({ proposals: [] });
    await fireEvent.click(button("Approve"));
    expect(onSend).toHaveBeenCalledWith("Approved.", "approved", "inbox-a");
  });
});
