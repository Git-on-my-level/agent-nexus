// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import {
  readWorkspaceView,
  writeWorkspaceView,
  clearWorkspaceViews,
} from "../../src/lib/workspaceViewCache.js";
afterEach(clearWorkspaceViews);
it("isolates snapshots by workspace and principal and expires them after 30 seconds", () => {
  writeWorkspaceView("org/personal/human", { count: 1 }, 100);
  writeWorkspaceView("org/omi/human", { count: 2 }, 100);
  expect(readWorkspaceView("org/personal/human", 200)).toEqual({ count: 1 });
  expect(readWorkspaceView("org/omi/other", 200)).toBeNull();
  expect(readWorkspaceView("org/personal/human", 30_100)).toBeNull();
  clearWorkspaceViews();
  expect(readWorkspaceView("org/omi/human", 200)).toBeNull();
});
