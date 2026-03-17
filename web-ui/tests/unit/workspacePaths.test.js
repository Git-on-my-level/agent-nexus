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
    const { appPath, workspacePath } = await loadWorkspacePaths("/oar");

    expect(appPath("/")).toBe("/oar");
    expect(appPath("/threads")).toBe("/oar/threads");
    expect(workspacePath("local")).toBe("/oar/local");
    expect(workspacePath("local", "/threads")).toBe("/oar/local/threads");
  });

  it("strips the configured base path before resolving workspace-relative paths", async () => {
    const { stripBasePath, stripWorkspacePath } =
      await loadWorkspacePaths("/oar");

    expect(stripBasePath("/oar/local/inbox")).toBe("/local/inbox");
    expect(stripWorkspacePath("/oar/local/inbox", "local")).toBe("/inbox");
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
