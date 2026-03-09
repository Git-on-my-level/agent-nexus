import { afterEach, describe, expect, it, vi } from "vitest";

async function loadProjectPaths(base = "") {
  vi.resetModules();
  vi.doMock("$app/paths", () => ({ base }));
  return import("../../src/lib/projectPaths.js");
}

afterEach(() => {
  vi.resetModules();
  vi.doUnmock("$app/paths");
});

describe("project paths", () => {
  it("prefixes app and project routes with the configured base path", async () => {
    const { appPath, projectPath } = await loadProjectPaths("/oar");

    expect(appPath("/")).toBe("/oar");
    expect(appPath("/threads")).toBe("/oar/threads");
    expect(projectPath("local")).toBe("/oar/local");
    expect(projectPath("local", "/threads")).toBe("/oar/local/threads");
  });

  it("strips the configured base path before resolving project-relative paths", async () => {
    const { stripBasePath, stripProjectPath } = await loadProjectPaths("/oar");

    expect(stripBasePath("/oar/local/inbox")).toBe("/local/inbox");
    expect(stripProjectPath("/oar/local/inbox", "local")).toBe("/inbox");
    expect(stripProjectPath("/local/inbox", "local")).toBe("/inbox");
  });

  it("keeps root-mounted behavior unchanged when no base path is configured", async () => {
    const { appPath, projectPath, stripProjectPath } = await loadProjectPaths();

    expect(appPath("/threads")).toBe("/threads");
    expect(projectPath("local", "/threads")).toBe("/local/threads");
    expect(stripProjectPath("/local/threads", "local")).toBe("/threads");
  });
});
