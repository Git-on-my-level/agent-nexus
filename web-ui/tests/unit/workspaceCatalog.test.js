import { afterEach, describe, expect, it, vi } from "vitest";
import {
  loadWorkspaceCatalog,
  toPublicWorkspaceCatalog,
} from "$lib/server/workspaceCatalog.js";

afterEach(() => {
  vi.resetModules();
});

describe("workspaceCatalog", () => {
  it("should parse ANX_WORKSPACES and ANX_DEFAULT_WORKSPACE env vars", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"}]',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("ws1");
    expect(catalog.devActorMode).toBe(false);
  });

  it("should parse ANX_DEFAULT_WORKSPACE", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"},{"slug":"ws2","label":"Workspace 2","coreBaseUrl":"http://localhost:8001"}]',
      ANX_DEFAULT_WORKSPACE: "ws2",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.defaultWorkspace.slug).toBe("ws2");
  });

  it("should preserve public origin metadata from ANX_WORKSPACES entries", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000","publicOrigin":"https://ws1.tailnet.ts.net/anx/ws1"},{"slug":"ws2","label":"Workspace 2","coreBaseUrl":"http://localhost:8001","public_origin":"https://ws2.tailnet.ts.net/anx/ws2"}]',
    };
    const catalog = loadWorkspaceCatalog(env);

    expect(catalog.workspaces[0].publicOrigin).toBe(
      "https://ws1.tailnet.ts.net/anx/ws1",
    );
    expect(catalog.workspaces[1].publicOrigin).toBe(
      "https://ws2.tailnet.ts.net/anx/ws2",
    );
  });

  it("should parse legacy ANX_PROJECTS env var", () => {
    const env = {
      ANX_PROJECTS:
        '[{"slug":"legacy1","label":"Legacy 1","coreBaseUrl":"http://localhost:8000"}]',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("legacy1");
  });

  it("should parse object-form ANX_WORKSPACES string URL entries", () => {
    const env = {
      ANX_WORKSPACES:
        '{"ws1":"http://localhost:8000","ws2":"http://localhost:8001"}',
      ANX_DEFAULT_WORKSPACE: "ws2",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(2);
    expect(catalog.workspaces[0].coreBaseUrl).toBe("http://localhost:8000");
    expect(catalog.workspaces[1].coreBaseUrl).toBe("http://localhost:8001");
    expect(catalog.defaultWorkspace.slug).toBe("ws2");
  });

  it("should parse legacy ANX_DEFAULT_PROJECT env var", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"}]',
      ANX_DEFAULT_PROJECT: "ws1",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.defaultWorkspace.slug).toBe("ws1");
  });

  it("should support devActorMode", () => {
    const env = {
      ANX_WORKSPACES: "[]",
      ANX_DEV_ACTOR_MODE: "true",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.devActorMode).toBe(true);
  });

  it("should fallback to single workspace when ANX_WORKSPACES is empty", () => {
    const env = {
      ANX_CORE_BASE_URL: "http://localhost:3000",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("local");
  });

  it("should allow an empty catalog for SaaS packed-host dev", () => {
    const env = {
      ANX_SAAS_PACKED_HOST_DEV: "1",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.defaultWorkspace).toBeNull();
    expect(catalog.workspaces).toHaveLength(0);
    expect(catalog.hostedDevEmpty).toBe(true);
  });

  it("should prefer ANX_SAAS_DEV_PRECONFIGURED_WORKSPACES when ANX_WORKSPACES is unset", () => {
    const env = {
      ANX_SAAS_PACKED_HOST_DEV: "1",
      ANX_SAAS_DEV_PRECONFIGURED_WORKSPACES:
        '[{"slug":"demo","label":"Demo","coreBaseUrl":"http://127.0.0.1:9000"}]',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.defaultWorkspace.slug).toBe("demo");
    expect(catalog.workspaces[0].coreBaseUrl).toBe("http://127.0.0.1:9000");
  });

  it("should throw a helpful error when ANX_WORKSPACES is not valid JSON", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000",}]',
    };
    expect(() => loadWorkspaceCatalog(env)).toThrow(/Example:.*"slug":"local"/);
    expect(() => loadWorkspaceCatalog(env)).toThrow(
      /fall back to ANX_CORE_BASE_URL/,
    );
  });

  it("should auto-repair missing } before ] after coreBaseUrl (common typo)", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"]',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("local");
    expect(catalog.workspaces[0].coreBaseUrl).toBe("http://127.0.0.1:8000");
  });

  it("should auto-repair swapped ]} after coreBaseUrl (common typo)", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"]}',
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

  it("should expose an empty public catalog when hosted dev has no default workspace", () => {
    const catalog = {
      defaultWorkspace: null,
      workspaces: [],
      workspaceBySlug: new Map(),
      devActorMode: false,
    };
    const publicCatalog = toPublicWorkspaceCatalog(catalog);
    expect(publicCatalog.defaultWorkspace).toBeNull();
    expect(publicCatalog.workspaces).toHaveLength(0);
  });
});
