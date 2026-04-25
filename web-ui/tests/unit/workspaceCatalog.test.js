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
        '[{"organizationSlug":"local","slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"}]',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("ws1");
    expect(catalog.devActorMode).toBe(false);
  });

  it("should parse ANX_DEFAULT_WORKSPACE", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"organizationSlug":"local","slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"},{"organizationSlug":"local","slug":"ws2","label":"Workspace 2","coreBaseUrl":"http://localhost:8001"}]',
      ANX_DEFAULT_WORKSPACE: "ws2",
      ANX_DEFAULT_ORGANIZATION: "local",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.defaultWorkspace.slug).toBe("ws2");
  });

  it("should preserve public origin metadata from ANX_WORKSPACES entries", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"organizationSlug":"acme","slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000","publicOrigin":"https://ws1.tailnet.ts.net/anx/ws1"},{"organizationSlug":"acme","slug":"ws2","label":"Workspace 2","coreBaseUrl":"http://localhost:8001","public_origin":"https://ws2.tailnet.ts.net/anx/ws2"}]',
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
        '[{"organizationSlug":"local","slug":"legacy1","label":"Legacy 1","coreBaseUrl":"http://localhost:8000"}]',
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
      ANX_DEFAULT_ORGANIZATION: "local",
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
        '[{"organizationSlug":"local","slug":"ws1","label":"Workspace 1","coreBaseUrl":"http://localhost:8000"}]',
      ANX_DEFAULT_PROJECT: "ws1",
      ANX_DEFAULT_ORGANIZATION: "local",
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

  it("ignores ANX_DEV_ACTOR_MODE in production-like builds (dev is false)", async () => {
    vi.resetModules();
    vi.doMock("$app/environment", () => ({ dev: false }));
    const { loadWorkspaceCatalog: loadProd } =
      await import("$lib/server/workspaceCatalog.js");
    const env = {
      ANX_WORKSPACES: "[]",
      ANX_DEV_ACTOR_MODE: "true",
    };
    const catalog = loadProd(env);
    expect(catalog.devActorMode).toBe(false);
  });

  it("should return an empty catalog when ANX_WORKSPACES is empty", () => {
    const env = {
      ANX_CORE_BASE_URL: "http://localhost:3000",
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(0);
    expect(catalog.defaultWorkspace).toBeNull();
  });

  it("should allow an empty catalog when resolver enables hosted empty-static mode", () => {
    const env = {};
    const catalog = loadWorkspaceCatalog(env, {
      allowsEmptyStaticCatalog: true,
    });
    expect(catalog.defaultWorkspace).toBeNull();
    expect(catalog.workspaces).toHaveLength(0);
    expect(catalog.allowsEmptyCatalog).toBe(true);
  });

  it("should prefer ANX_SAAS_DEV_PRECONFIGURED_WORKSPACES when ANX_WORKSPACES is unset", () => {
    const env = {
      ANX_SAAS_DEV_PRECONFIGURED_WORKSPACES:
        '[{"organizationSlug":"local","slug":"demo","label":"Demo","coreBaseUrl":"http://127.0.0.1:9000"}]',
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
    expect(() => loadWorkspaceCatalog(env)).toThrow(
      /Example:.*"organizationSlug":"local"/,
    );
    expect(() => loadWorkspaceCatalog(env)).toThrow(/empty catalog/);
  });

  it("should auto-repair missing } before ] after coreBaseUrl (common typo)", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"organizationSlug":"local","slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"]',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("local");
    expect(catalog.workspaces[0].coreBaseUrl).toBe("http://127.0.0.1:8000");
  });

  it("should auto-repair swapped ]} after coreBaseUrl (common typo)", () => {
    const env = {
      ANX_WORKSPACES:
        '[{"organizationSlug":"local","slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"]}',
    };
    const catalog = loadWorkspaceCatalog(env);
    expect(catalog.workspaces).toHaveLength(1);
    expect(catalog.defaultWorkspace.slug).toBe("local");
  });
});

describe("toPublicWorkspaceCatalog", () => {
  it("should expose devActorMode in public catalog", () => {
    const catalog = {
      defaultWorkspace: {
        organizationSlug: "local",
        slug: "test",
        label: "Test",
        description: "",
      },
      workspaces: [
        {
          organizationSlug: "local",
          slug: "test",
          label: "Test",
          description: "",
        },
      ],
      workspaceByComposite: new Map(),
      devActorMode: true,
    };
    const publicCatalog = toPublicWorkspaceCatalog(catalog);
    expect(publicCatalog.devActorMode).toBe(true);
  });

  it("should default devActorMode to false when not set", () => {
    const catalog = {
      defaultWorkspace: {
        organizationSlug: "local",
        slug: "test",
        label: "Test",
        description: "",
      },
      workspaces: [
        {
          organizationSlug: "local",
          slug: "test",
          label: "Test",
          description: "",
        },
      ],
      workspaceByComposite: new Map(),
    };
    const publicCatalog = toPublicWorkspaceCatalog(catalog);
    expect(publicCatalog.devActorMode).toBe(false);
  });

  it("should expose an empty public catalog when hosted dev has no default workspace", () => {
    const catalog = {
      defaultWorkspace: null,
      workspaces: [],
      workspaceByComposite: new Map(),
      devActorMode: false,
    };
    const publicCatalog = toPublicWorkspaceCatalog(catalog);
    expect(publicCatalog.defaultWorkspace).toBeNull();
    expect(publicCatalog.workspaces).toHaveLength(0);
  });
});
