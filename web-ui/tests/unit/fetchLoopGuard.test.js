import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { installFetchLoopGuard } from "../../src/lib/dev/fetchLoopGuard.js";

describe("fetchLoopGuard", () => {
  /** @type {() => void} */
  let uninstall;
  let originalFetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async () => new Response("ok", { status: 200 }));
  });

  afterEach(() => {
    if (uninstall) uninstall();
    globalThis.fetch = originalFetch;
  });

  it("allows bounded request rates without tripping", async () => {
    const onTrip = vi.fn();
    uninstall = installFetchLoopGuard({
      windowMs: 1000,
      maxPerWindow: 10,
      onTrip,
    });

    for (let i = 0; i < 10; i += 1) {
      await globalThis.fetch("/api/things");
    }
    expect(onTrip).not.toHaveBeenCalled();
  });

  it("trips when the same URL exceeds the threshold within the window", async () => {
    const onTrip = vi.fn();
    uninstall = installFetchLoopGuard({
      windowMs: 1000,
      maxPerWindow: 5,
      onTrip,
    });

    for (let i = 0; i < 6; i += 1) {
      await globalThis.fetch("/auth/session");
    }

    expect(onTrip).toHaveBeenCalledTimes(1);
    const [message, info] = onTrip.mock.calls[0];
    expect(message).toMatch(/runaway request loop/i);
    expect(message).toMatch(/\/auth\/session/);
    expect(info.count).toBeGreaterThan(5);
    expect(info.maxPerWindow).toBe(5);
  });

  it("does not trip from unrelated endpoints sharing a window", async () => {
    const onTrip = vi.fn();
    uninstall = installFetchLoopGuard({
      windowMs: 1000,
      maxPerWindow: 5,
      onTrip,
    });

    for (let i = 0; i < 5; i += 1) {
      await globalThis.fetch("/auth/session");
      await globalThis.fetch("/api/inbox");
      await globalThis.fetch("/api/topics");
    }

    expect(onTrip).not.toHaveBeenCalled();
  });

  it("only trips once per URL until the window clears", async () => {
    const onTrip = vi.fn();
    uninstall = installFetchLoopGuard({
      windowMs: 1000,
      maxPerWindow: 3,
      onTrip,
    });

    for (let i = 0; i < 20; i += 1) {
      await globalThis.fetch("/auth/session");
    }
    expect(onTrip).toHaveBeenCalledTimes(1);
  });

  it("uninstall restores the original fetch", async () => {
    const onTrip = vi.fn();
    const original = globalThis.fetch;
    uninstall = installFetchLoopGuard({
      windowMs: 1000,
      maxPerWindow: 1,
      onTrip,
    });
    expect(globalThis.fetch).not.toBe(original);
    uninstall();
    expect(globalThis.fetch).toBe(original);
  });

  it("default behavior throws when tripped", async () => {
    uninstall = installFetchLoopGuard({
      windowMs: 1000,
      maxPerWindow: 2,
    });

    // Suppress expected console.error from the default trip handler so this
    // doesn't clutter test output.
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});

    await globalThis.fetch("/api/x");
    await globalThis.fetch("/api/x");
    await expect(globalThis.fetch("/api/x")).rejects.toThrow(
      /runaway request loop/i,
    );

    consoleError.mockRestore();
  });
});
