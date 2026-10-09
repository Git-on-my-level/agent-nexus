import { afterEach, expect, it, vi } from "vitest";
import { reliableRead } from "../../src/lib/reliableRead.js";
afterEach(() => vi.useRealTimers());
it("recovers a 504 without exposing a terminal failure", async () => {
  vi.useFakeTimers();
  const read = vi
    .fn()
    .mockRejectedValueOnce(Object.assign(new Error("gateway"), { status: 504 }))
    .mockResolvedValue({ items: [1] });
  const retry = vi.fn();
  const pending = reliableRead(read, { onRetry: retry });
  await vi.advanceTimersByTimeAsync(1000);
  expect(await pending).toEqual({ items: [1] });
  expect(retry).toHaveBeenCalledTimes(1);
  expect(read).toHaveBeenCalledTimes(2);
  expect(vi.getTimerCount()).toBe(0);
});
it("surfaces sustained network failure after the shared thirty-second window", async () => {
  vi.useFakeTimers();
  const read = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
  const pending = reliableRead(read);
  const assertion = expect(pending).rejects.toThrow("Failed to fetch");
  await vi.advanceTimersByTimeAsync(40_000);
  await assertion;
  expect(read.mock.calls.length).toBeLessThan(10);
  expect(vi.getTimerCount()).toBe(0);
});
it("does not retry a real 4xx or a malformed payload", async () => {
  for (const error of [
    Object.assign(new Error("denied"), { status: 403 }),
    new Error("invalid payload"),
  ]) {
    const read = vi.fn().mockRejectedValue(error);
    await expect(reliableRead(read)).rejects.toBe(error);
    expect(read).toHaveBeenCalledTimes(1);
  }
});
it("cancels a pending read and releases its deadline", async () => {
  vi.useFakeTimers();
  const controller = new AbortController();
  const pending = reliableRead(() => new Promise(() => {}), {
    signal: controller.signal,
  });
  const assertion = expect(pending).rejects.toThrow("left page");
  controller.abort(new Error("left page"));
  await assertion;
  expect(vi.getTimerCount()).toBe(0);
});
it("allows a ten-second cold read and bounds a stalled request", async () => {
  vi.useFakeTimers();
  const pending = reliableRead(
    () => new Promise((resolve) => setTimeout(() => resolve("awake"), 10_000)),
  );
  await vi.advanceTimersByTimeAsync(10_000);
  expect(await pending).toBe("awake");
  const stalled = reliableRead(() => new Promise(() => {}));
  const assertion = expect(stalled).rejects.toThrow("Loading timed out");
  await vi.advanceTimersByTimeAsync(45_000);
  await assertion;
});

it("retries the core client's wrapped Safari transport failure", async () => {
  vi.useFakeTimers();
  const read = vi
    .fn()
    .mockRejectedValueOnce(
      new Error("Unable to reach anx-core at workspace: Load failed"),
    )
    .mockResolvedValue("ok");
  const pending = reliableRead(read);
  await vi.advanceTimersByTimeAsync(1000);
  expect(await pending).toBe("ok");
});

it("does not grant a fresh forty-five seconds to a retry near the failure deadline", async () => {
  vi.useFakeTimers();
  const read = vi
    .fn()
    .mockRejectedValueOnce(
      Object.assign(new Error("unavailable"), { status: 503 }),
    )
    .mockImplementation(() => new Promise(() => {}));
  const pending = reliableRead(read);
  const assertion = expect(pending).rejects.toThrow("Loading timed out");
  await vi.advanceTimersByTimeAsync(30_000);
  await assertion;
  expect(vi.getTimerCount()).toBe(0);
});
