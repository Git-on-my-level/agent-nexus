// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import {
  readWorkspaceView,
  writeWorkspaceView,
  clearWorkspaceViews,
  workspaceViewRevision,
  onWorkspaceViewsDenied,
} from "../../src/lib/workspaceViewCache.js";
import { reliableRead } from "../../src/lib/reliableRead.js";
afterEach(clearWorkspaceViews);
it.each([401, 403])(
  "a %s read purges every scoped view and notifies mounted consumers without retrying",
  async (status) => {
    for (const view of ["inbox", "overview", "tasks:a", "tasks:b"])
      writeWorkspaceView(`reader:${view}`, { restricted: true });
    writeWorkspaceView("other:tasks:a", { restricted: false });
    const revoked = vi.fn();
    const stop = onWorkspaceViewsDenied("reader", revoked);
    const revision = workspaceViewRevision();
    const error = Object.assign(new Error("denied"), {
      coreHttpStatus: status,
    });
    const read = vi.fn().mockRejectedValue(error);
    await expect(reliableRead(read, { cacheScope: "reader" })).rejects.toBe(
      error,
    );
    expect(read).toHaveBeenCalledTimes(1);
    expect(revoked).toHaveBeenCalledWith(error);
    expect(workspaceViewRevision()).toBeGreaterThan(revision);
    for (const view of ["inbox", "overview", "tasks:a", "tasks:b"])
      expect(readWorkspaceView(`reader:${view}`)).toBeNull();
    expect(readWorkspaceView("other:tasks:a")).toEqual({ restricted: false });
    expect(localStorage.getItem("anx.workspace-views.v1")).not.toContain(
      "reader:",
    );
    stop();
  },
);
it("isolates snapshots by workspace and principal and retains them through a transient and expires after a day", () => {
  writeWorkspaceView("org/personal/human", { count: 1 }, 100);
  writeWorkspaceView("org/demo/human", { count: 2 }, 100);
  expect(readWorkspaceView("org/personal/human", 200)).toEqual({ count: 1 });
  expect(readWorkspaceView("org/demo/other", 200)).toBeNull();
  expect(readWorkspaceView("org/personal/human", 86_400_100)).toBeNull();
  clearWorkspaceViews();
  expect(readWorkspaceView("org/demo/human", 200)).toBeNull();
});

it("persists a versioned snapshot and removes storage at sign-out", () => {
  writeWorkspaceView("reader:inbox", { items: ["ask"] });
  expect(
    JSON.parse(localStorage.getItem("anx.workspace-views.v1")),
  ).toMatchObject({
    version: 1,
    entries: [["reader:inbox", { value: { items: ["ask"] } }]],
  });
  clearWorkspaceViews();
  expect(localStorage.getItem("anx.workspace-views.v1")).toBeNull();
});
it("bounds total cache size and tolerates unavailable storage", () => {
  writeWorkspaceView("large", { body: "a".repeat(2_000_001) });
  expect(localStorage.getItem("anx.workspace-views.v1").length).toBeLessThan(
    2_000_000,
  );
  for (let i = 0; i < 20; i++) writeWorkspaceView(`reader:${i}`, { i });
  expect(readWorkspaceView("reader:0")).toBeNull();
  expect(readWorkspaceView("reader:19")).toEqual({ i: 19 });
});
it("hydrates persisted snapshots after the module is reloaded", async () => {
  writeWorkspaceView("reader:overview", { count: 7 });
  vi.resetModules();
  const fresh = await import("../../src/lib/workspaceViewCache.js");
  expect(fresh.readWorkspaceView("reader:overview")).toEqual({ count: 7 });
  fresh.clearWorkspaceViews();
});
it("keeps navigation usable when browser storage throws", () => {
  const spy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("storage blocked");
  });
  writeWorkspaceView("reader:tasks", { records: [1] });
  expect(readWorkspaceView("reader:tasks")).toEqual({ records: [1] });
  spy.mockRestore();
});
