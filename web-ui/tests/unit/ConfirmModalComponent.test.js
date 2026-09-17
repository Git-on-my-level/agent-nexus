// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import ConfirmModal from "../../src/lib/components/ConfirmModal.svelte";

describe("ConfirmModal", () => {
  afterEach(() => cleanup());

  it("shows an error and a named busy label", () => {
    render(ConfirmModal, {
      open: true,
      title: "Revoke principal",
      confirmLabel: "Confirm revoke",
      busy: true,
      busyLabel: "Revoking…",
      error: "revocation rejected by policy",
    });

    expect(screen.getByRole("alert").textContent).toContain(
      "revocation rejected by policy",
    );
    const confirm = screen.getByRole("button", { name: "Revoking…" });
    expect(confirm.disabled).toBe(true);
  });

  it("keeps confirm disabled until typed confirmation matches and it is not blocked", async () => {
    const onconfirm = vi.fn();
    const { rerender } = render(ConfirmModal, {
      open: true,
      title: "Last active human principal",
      confirmLabel: "Allow human lockout and revoke",
      typedConfirmation: "agent-123",
      confirmBlocked: true,
      onconfirm,
    });

    const confirm = screen.getByRole("button", {
      name: "Allow human lockout and revoke",
    });
    const typed = screen.getByRole("textbox");
    await fireEvent.input(typed, { target: { value: "agent-123" } });
    expect(confirm.disabled).toBe(true);

    // Enter must not bypass the block either.
    await fireEvent.keyDown(typed, { key: "Enter" });
    expect(onconfirm).not.toHaveBeenCalled();

    await rerender({ confirmBlocked: false });
    expect(typed.value).toBe("agent-123");
    expect(confirm.disabled).toBe(false);
    await fireEvent.keyDown(typed, { key: "Enter" });
    expect(onconfirm).toHaveBeenCalledTimes(1);
  });
});
