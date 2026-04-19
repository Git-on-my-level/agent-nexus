import { afterEach, describe, expect, it, vi } from "vitest";

async function loadWorkspacePaths(base = "") {
  vi.resetModules();
  vi.doMock("$app/paths", () => ({ base }));
  return import("../../src/lib/workspacePaths.js");
}

afterEach(() => {
  vi.resetModules();
  vi.doUnmock("$app/paths");
});

describe("workspace paths", () => {
  it("prefixes app and workspace routes with the configured base path", async () => {
    const { appPath, workspacePath } = await loadWorkspacePaths("/anx");

    expect(appPath("/")).toBe("/anx");
    expect(appPath("/threads")).toBe("/anx/threads");
    expect(workspacePath("local", "ws1")).toBe("/anx/o/local/w/ws1");
    expect(workspacePath("local", "ws1", "/threads")).toBe(
      "/anx/o/local/w/ws1/threads",
    );
  });

  it("strips the configured base path before resolving workspace-relative paths", async () => {
    const { stripBasePath, stripWorkspacePath } =
      await loadWorkspacePaths("/anx");

    expect(stripBasePath("/anx/o/local/w/ws/inbox")).toBe(
      "/o/local/w/ws/inbox",
    );
    expect(stripWorkspacePath("/anx/o/local/w/ws/inbox", "local", "ws")).toBe(
      "/inbox",
    );
    expect(stripWorkspacePath("/o/local/w/ws/inbox", "local", "ws")).toBe(
      "/inbox",
    );
  });

  it("keeps root-mounted behavior unchanged when no base path is configured", async () => {
    const { appPath, workspacePath, stripWorkspacePath } =
      await loadWorkspacePaths();

    expect(appPath("/threads")).toBe("/threads");
    expect(workspacePath("local", "ws", "/threads")).toBe(
      "/o/local/w/ws/threads",
    );
    expect(stripWorkspacePath("/o/local/w/ws/threads", "local", "ws")).toBe(
      "/threads",
    );
  });

  it("requires slugs for workspace storage keys", async () => {
    const { buildWorkspaceStorageKey } = await loadWorkspacePaths();
    expect(buildWorkspaceStorageKey("oar_ui_actor", "alpha")).toBe(
      "oar_ui_actor:alpha",
    );
    expect(() => buildWorkspaceStorageKey("oar_ui_actor", "")).toThrow(
      /workspace slug is required/,
    );
  });
});
