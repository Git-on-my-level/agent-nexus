import { beforeEach, describe, expect, it, vi } from "vitest";

import { createRedirectLoopGuard } from "../../src/lib/dev/redirectLoopGuard.js";

describe("redirectLoopGuard", () => {
  let now;
  /** @returns {() => number} */
  function makeNow() {
    let t = 0;
    now = (delta = 0) => {
      t += delta;
      return t;
    };
    return () => now(0);
  }

  beforeEach(() => {
    makeNow();
  });

  it("allows redirects up to the threshold", () => {
    const onTrip = vi.fn();
    const guard = createRedirectLoopGuard({
      windowMs: 1000,
      maxPerWindow: 5,
      onTrip,
      now: makeNow(),
    });

    for (let i = 0; i < 5; i += 1) {
      now(1);
      expect(guard.shouldNavigate("/login")).toBe(true);
    }
    expect(onTrip).not.toHaveBeenCalled();
  });

  it("suppresses navigation once the threshold is crossed", () => {
    const onTrip = vi.fn();
    const guard = createRedirectLoopGuard({
      windowMs: 1000,
      maxPerWindow: 3,
      onTrip,
      now: makeNow(),
    });

    expect(guard.shouldNavigate("/login")).toBe(true);
    expect(guard.shouldNavigate("/login")).toBe(true);
    expect(guard.shouldNavigate("/login")).toBe(true);
    expect(guard.shouldNavigate("/login")).toBe(false);
    expect(guard.shouldNavigate("/login")).toBe(false);
    expect(onTrip).toHaveBeenCalledTimes(1);
    const [message, info] = onTrip.mock.calls[0];
    expect(message).toMatch(/navigation loop detected/i);
    expect(info.destination).toBe("/login");
    expect(info.maxPerWindow).toBe(3);
    expect(info.count).toBeGreaterThan(3);
  });

  it("only fires onTrip once per cooldown even if the loop continues", () => {
    const onTrip = vi.fn();
    const guard = createRedirectLoopGuard({
      windowMs: 1000,
      maxPerWindow: 2,
      cooldownMs: 5000,
      onTrip,
      now: makeNow(),
    });

    for (let i = 0; i < 50; i += 1) {
      now(10);
      guard.shouldNavigate("/login");
    }
    expect(onTrip).toHaveBeenCalledTimes(1);
  });

  it("tracks destinations independently", () => {
    const onTrip = vi.fn();
    const guard = createRedirectLoopGuard({
      windowMs: 1000,
      maxPerWindow: 2,
      onTrip,
      now: makeNow(),
    });

    expect(guard.shouldNavigate("/login")).toBe(true);
    expect(guard.shouldNavigate("/login")).toBe(true);
    expect(guard.shouldNavigate("/")).toBe(true);
    expect(guard.shouldNavigate("/")).toBe(true);
    expect(guard.shouldNavigate("/login")).toBe(false);
    expect(guard.shouldNavigate("/")).toBe(false);
    expect(onTrip).toHaveBeenCalledTimes(2);
    expect(onTrip.mock.calls[0][1].destination).toBe("/login");
    expect(onTrip.mock.calls[1][1].destination).toBe("/");
  });

  it("recovers once enough time passes for old timestamps to age out", () => {
    const onTrip = vi.fn();
    const guard = createRedirectLoopGuard({
      windowMs: 1000,
      maxPerWindow: 2,
      onTrip,
      now: makeNow(),
    });

    expect(guard.shouldNavigate("/login")).toBe(true);
    expect(guard.shouldNavigate("/login")).toBe(true);
    expect(guard.shouldNavigate("/login")).toBe(false);

    now(2000);

    expect(guard.shouldNavigate("/login")).toBe(true);
  });

  it("reset() clears state for a single destination", () => {
    const onTrip = vi.fn();
    const guard = createRedirectLoopGuard({
      windowMs: 1000,
      maxPerWindow: 2,
      onTrip,
      now: makeNow(),
    });

    guard.shouldNavigate("/login");
    guard.shouldNavigate("/login");
    guard.shouldNavigate("/login");
    expect(guard.count("/login")).toBeGreaterThan(2);

    guard.reset("/login");
    expect(guard.count("/login")).toBe(0);
    expect(guard.shouldNavigate("/login")).toBe(true);
  });

  it("swallows onTrip throws so the navigation suppression path always runs", () => {
    const onTrip = vi.fn(() => {
      throw new Error("test");
    });
    const guard = createRedirectLoopGuard({
      windowMs: 1000,
      maxPerWindow: 1,
      onTrip,
      now: makeNow(),
    });

    expect(guard.shouldNavigate("/login")).toBe(true);
    expect(() => guard.shouldNavigate("/login")).not.toThrow();
    expect(onTrip).toHaveBeenCalledTimes(1);
  });

  it("default onTrip writes to console.error", () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => {});
    const guard = createRedirectLoopGuard({
      windowMs: 1000,
      maxPerWindow: 1,
      now: makeNow(),
    });

    guard.shouldNavigate("/login");
    guard.shouldNavigate("/login");

    expect(consoleError).toHaveBeenCalled();
    consoleError.mockRestore();
  });
});
