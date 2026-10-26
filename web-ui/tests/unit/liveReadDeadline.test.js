import { afterEach, expect, it, vi } from "vitest";

import { readWithDeadline } from "../../src/lib/liveReports.js";

afterEach(() => vi.useRealTimers());

it("settles a read that never answers, and aborts it", async () => {
  vi.useFakeTimers();
  let seen;
  const read = readWithDeadline(
    (signal) => {
      seen = signal;
      return new Promise(() => {});
    },
    { ms: 1000 },
  );
  const outcome = read.catch((error) => error);
  await vi.advanceTimersByTimeAsync(1000);
  const error = await outcome;
  expect(error.name).toBe("TimeoutError");
  expect(seen.aborted).toBe(true);
});

it("passes a prompt answer through", async () => {
  await expect(readWithDeadline(async () => "ok", { ms: 1000 })).resolves.toBe(
    "ok",
  );
});

it("cancels when the caller closes", async () => {
  const closing = new AbortController();
  let seen;
  const read = readWithDeadline(
    (signal) => {
      seen = signal;
      return new Promise(() => {});
    },
    { signal: closing.signal },
  );
  closing.abort(new Error("closed"));
  await expect(read).rejects.toThrow("closed");
  expect(seen.aborted).toBe(true);
});
