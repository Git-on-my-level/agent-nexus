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
