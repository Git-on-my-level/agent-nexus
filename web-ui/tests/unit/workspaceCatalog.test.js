import { afterEach, describe, expect, it, vi } from "vitest";
import {
  loadWorkspaceCatalog,
  toPublicWorkspaceCatalog,
} from "$lib/server/workspaceCatalog.js";

afterEach(() => {
  vi.resetModules();
});

describe("workspaceCatalog", () => {
  it("should parse OAR_WORKSPACES and OAR_DEFAULT_WORKSPACE env vars", () => {
    const env = {
      OAR_WORKSPACES:
        '[{"slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"}]',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("ws1");
    expect(catalog.devActorMode).toBe(false);
  });

  it("should parse OAR_DEFAULT_WORKSPACE", () => {
    const env = {
      OAR_WORKSPACES:
        '[{"slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"},{"slug":"ws2","label":"Workspace 2","coreBaseUrl":"http://localhost:8001"}]',
      OAR_DEFAULT_WORKSPACE: "ws2",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.defaultWorkspace.slug).toBe("ws2");
  });

  it("should parse legacy OAR_PROJECTS env var", () => {
    const env = {
      OAR_PROJECTS:
        '[{"slug":"legacy1","label":"Legacy 1","coreBaseUrl":"http://localhost:8000"}]',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("legacy1");
  });

  it("should parse object-form OAR_WORKSPACES string URL entries", () => {
    const env = {
      OAR_WORKSPACES:
        '{"ws1":"http://localhost:8000","ws2":"http://localhost:8001"}',
      OAR_DEFAULT_WORKSPACE: "ws2",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(2);
    expect(catalog.workspaces[0].coreBaseUrl).toBe("http://localhost:8000");
    expect(catalog.workspaces[1].coreBaseUrl).toBe("http://localhost:8001");
    expect(catalog.defaultWorkspace.slug).toBe("ws2");
  });

  it("should parse legacy OAR_DEFAULT_PROJECT env var", () => {
    const env = {
      OAR_WORKSPACES:
        '[{"slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"}]',
      OAR_DEFAULT_PROJECT: "ws1",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.defaultWorkspace.slug).toBe("ws1");
  });

  it("should support devActorMode", () => {
    const env = {
      OAR_WORKSPACES: "[]",
      OAR_DEV_ACTOR_MODE: "true",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.devActorMode).toBe(true);
  });

  it("should fallback to single workspace when OAR_WORKSPACES is empty", () => {
    const env = {
      OAR_CORE_BASE_URL: "http://localhost:3000",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("local");
  });
});

describe("toPublicWorkspaceCatalog", () => {
  it("should expose devActorMode in public catalog", () => {
    const catalog = {
      defaultWorkspace: { slug: "test", label: "Test", description: "" },
      workspaces: [{ slug: "test", label: "Test", description: "" }],
      workspaceBySlug: new Map(),
      devActorMode: true,
    };
    const publicCatalog = toPublicWorkspaceCatalog(catalog);
    expect(publicCatalog.devActorMode).toBe(true);
  });

  it("should default devActorMode to false when not set", () => {
    const catalog = {
      defaultWorkspace: { slug: "test", label: "Test", description: "" },
      workspaces: [{ slug: "test", label: "Test", description: "" }],
      workspaceBySlug: new Map(),
    };
    const publicCatalog = toPublicWorkspaceCatalog(catalog);
    expect(publicCatalog.devActorMode).toBe(false);
  });
});
