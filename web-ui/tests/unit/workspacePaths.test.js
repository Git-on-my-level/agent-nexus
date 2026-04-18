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
    expect(workspacePath("local")).toBe("/anx/local");
    expect(workspacePath("local", "/threads")).toBe("/anx/local/threads");
  });

  it("strips the configured base path before resolving workspace-relative paths", async () => {
    const { stripBasePath, stripWorkspacePath } =
      await loadWorkspacePaths("/anx");

    expect(stripBasePath("/anx/local/inbox")).toBe("/local/inbox");
    expect(stripWorkspacePath("/anx/local/inbox", "local")).toBe("/inbox");
    expect(stripWorkspacePath("/local/inbox", "local")).toBe("/inbox");
  });

  it("keeps root-mounted behavior unchanged when no base path is configured", async () => {
    const { appPath, workspacePath, stripWorkspacePath } =
      await loadWorkspacePaths();

    expect(appPath("/threads")).toBe("/threads");
    expect(workspacePath("local", "/threads")).toBe("/local/threads");
    expect(stripWorkspacePath("/local/threads", "local")).toBe("/threads");
  });
});
