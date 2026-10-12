// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { staleWhileRevalidate } from "../../src/lib/staleWhileRevalidate.js";
import {
  clearWorkspaceViews,
  readWorkspaceView,
} from "../../src/lib/workspaceViewCache.js";

afterEach(() => {
  clearWorkspaceViews();
  vi.useRealTimers();
});
function setup(read, currentScope = () => "reader") {
  return staleWhileRevalidate({
    scope: "reader",
    key: "reader:overview",
    currentScope,
    read,
    readOptions: { failureWindowMs: 0 },
  });
}
it("keeps a snapshot during slow reads, coalesces wakes, retains it on failure and retries", async () => {
  let finish;
  const read = vi
    .fn()
    .mockResolvedValueOnce({ title: "old" })
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockRejectedValueOnce(new Error("offline"))
    .mockResolvedValueOnce({ title: "new" });
  const view = setup(read);
  await view.refresh();
  const pending = view.refresh();
  expect(view.refresh()).toBe(pending);
  expect(get(view)).toMatchObject({ data: { title: "old" }, refreshing: true });
  await vi.waitFor(() => expect(finish).toBeTypeOf("function"));
  finish({ title: "middle" });
  await pending;
  await view.refresh();
  expect(get(view)).toMatchObject({
    data: { title: "middle" },
    error: "offline",
    refreshing: false,
  });
  await view.refresh();
  expect(get(view)).toMatchObject({ data: { title: "new" }, error: "" });
  view.destroy();
});
it("rejects late results after a principal/workspace change", async () => {
  let scope = "reader";
  let finish;
  const view = setup(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
    () => scope,
  );
  const pending = view.refresh();
  await vi.waitFor(() => expect(finish).toBeTypeOf("function"));
  scope = "other";
  finish({ secret: true });
  await pending;
  expect(readWorkspaceView("reader:overview")).toBeNull();
  view.destroy();
});
it.each([401, 403])("revokes existing data after %s", async (status) => {
  const view = setup(
    vi
      .fn()
      .mockResolvedValueOnce({ private: true })
      .mockRejectedValueOnce(Object.assign(new Error("denied"), { status })),
  );
  await view.refresh();
  await view.refresh();
  expect(get(view)).toMatchObject({
    data: null,
    error: "denied",
    refreshing: false,
  });
  view.destroy();
});
it("keeps a first-load error visible while a retry is in flight", async () => {
  let finish;
  const read = vi
    .fn()
    .mockRejectedValueOnce(new Error("unavailable"))
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
  const view = setup(read);
  await view.refresh();
  expect(get(view)).toMatchObject({
    data: null,
    error: "unavailable",
    refreshing: false,
  });
  const pending = view.refresh();
  expect(get(view)).toMatchObject({
    data: null,
    error: "unavailable",
    refreshing: true,
  });
  await vi.waitFor(() => expect(finish).toBeTypeOf("function"));
  finish({ title: "ok" });
  await pending;
  expect(get(view)).toMatchObject({
    data: { title: "ok" },
    error: "",
    refreshing: false,
  });
  view.destroy();
});

it("does not auto-refresh after a confirmed denial", async () => {
  const read = vi
    .fn()
    .mockRejectedValueOnce(Object.assign(new Error("denied"), { status: 401 }))
    .mockResolvedValue({ title: "ok" });
  const view = setup(read);
  let notify = () => {};
  const stop = view.mount({
    connect: (onChange) => {
      notify = onChange;
      return () => {};
    },
  });
  await vi.waitFor(() =>
    expect(get(view)).toMatchObject({
      data: null,
      error: "denied",
      refreshing: false,
    }),
  );
  const calls = read.mock.calls.length;
  notify();
  document.dispatchEvent(new Event("visibilitychange"));
  window.dispatchEvent(new Event("online"));
  expect(read).toHaveBeenCalledTimes(calls);
  expect(get(view)).toMatchObject({
    data: null,
    error: "denied",
    refreshing: false,
  });
  await view.refresh();
  expect(get(view)).toMatchObject({ data: { title: "ok" }, error: "" });
  stop();
  view.destroy();
});

it("returns an actionable first-load failure and enforces the return freshness window", async () => {
  vi.useFakeTimers();
  const read = vi
    .fn()
    .mockRejectedValueOnce(new Error("unavailable"))
    .mockResolvedValue({ title: "ok" });
  const view = setup(read);
  await view.refresh();
  expect(get(view)).toMatchObject({ data: null, error: "unavailable" });
  await view.refresh();
  await view.refresh({ force: false });
  expect(read).toHaveBeenCalledTimes(2);
  vi.advanceTimersByTime(31_000);
  await view.refresh({ force: false });
  expect(read).toHaveBeenCalledTimes(3);
  view.destroy();
});

it("bounds a silent auth refresh that never answers", async () => {
  vi.useFakeTimers();
  const read = vi.fn();
  const view = staleWhileRevalidate({
    scope: "reader",
    key: "reader:overview",
    currentScope: () => "reader",
    beforeRead: () => new Promise(() => {}),
    read,
    readOptions: { attemptMs: 50, failureWindowMs: 0 },
  });
  const pending = view.refresh();
  await vi.advanceTimersByTimeAsync(51);
  await pending;
  expect(get(view)).toMatchObject({
    data: null,
    error: "Loading timed out",
    refreshing: false,
  });
  expect(read).not.toHaveBeenCalled();
  view.destroy();
});

it("a confirmed mutation supersedes older reads without losing the new flight", async () => {
  let finishOld, finishNew, oldSignal;
  const read = vi
    .fn()
    .mockResolvedValueOnce({ pinned: "initial" })
    .mockImplementationOnce((signal) => {
      oldSignal = signal;
      return new Promise((resolve) => {
        finishOld = resolve;
      });
    })
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishNew = resolve;
        }),
    );
  const view = setup(read);
  await view.refresh();
  const oldFlight = view.refresh();
  await vi.waitFor(() => expect(finishOld).toBeTypeOf("function"));
  const newFlight = view.refresh({ replace: true });
  await vi.waitFor(() => expect(finishNew).toBeTypeOf("function"));
  expect(oldSignal.aborted).toBe(true);
  expect(await oldFlight).toBe(false);
  expect(view.refresh()).toBe(newFlight);
  finishNew({ pinned: "confirmed" });
  await newFlight;
  finishOld({ pinned: "initial" });
  expect(get(view).data).toEqual({ pinned: "confirmed" });
  expect(readWorkspaceView("reader:overview")).toEqual({ pinned: "confirmed" });
  view.destroy();
});

it.each([500, 401, 403])(
  "reports one return failure for %s, including denial before cache revocation",
  async (status) => {
    const onReturnFailure = vi.fn();
    const error = Object.assign(new Error("private message"), { status });
    const view = staleWhileRevalidate({
      scope: "reader",
      key: "reader:overview",
      currentScope: () => "reader",
      read: vi
        .fn()
        .mockResolvedValueOnce({ private: true })
        .mockRejectedValue(error),
      onReturnFailure,
      readOptions: { failureWindowMs: 0 },
    });
    await view.refresh();
    const updatedAt = get(view).updatedAt;
    const pending = view.refresh({ returning: true });
    expect(view.refresh({ returning: true })).toBe(pending);
    await pending;
    expect(onReturnFailure).toHaveBeenCalledTimes(1);
    expect(onReturnFailure).toHaveBeenCalledWith(error, updatedAt);
    await view.refresh();
    expect(onReturnFailure).toHaveBeenCalledTimes(1);
    view.destroy();
  },
);
