// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import HostedSessionErrorPanel from "../../src/lib/hosted/HostedSessionErrorPanel.svelte";

afterEach(cleanup);

describe("HostedSessionErrorPanel", () => {
  it("renders message, support link, retry, and sign out", async () => {
    const onretry = vi.fn();
    const onsignout = vi.fn();

    render(HostedSessionErrorPanel, {
      message: "Control plane timeout.",
      onretry,
      onsignout,
    });

    expect(screen.getByText(/Control plane timeout/)).toBeTruthy();
    const support = screen.getByRole("link", { name: /contact support/i });
    expect(support.getAttribute("href")).toBe("mailto:support@agentnexus.com");

    const retry = screen.getByRole("button", { name: /Retry/i });
    retry.click();
    expect(onretry).toHaveBeenCalledTimes(1);

    const signout = screen.getByRole("button", { name: /Sign out/i });
    signout.click();
    expect(onsignout).toHaveBeenCalledTimes(1);
  });
});
