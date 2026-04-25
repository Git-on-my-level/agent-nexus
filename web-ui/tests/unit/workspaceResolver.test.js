import { beforeEach, describe, expect, it, vi } from "vitest";

const mockState = vi.hoisted(() => ({
  env: {},
}));

vi.mock("$env/dynamic/private", () => ({
  env: mockState.env,
}));

import {
  clearWorkspaceResolutionCache,
  getWorkspaceResolutionCacheSize,
  resolveWorkspaceCatalog,
  resolveProxyWorkspaceTarget,
  resolveWorkspaceInRoute,
} from "../../src/lib/server/workspaceResolver.js";

function createEvent() {
  return {
    params: {},
    request: new Request("https://anx.example.test/api/threads", {
      headers: {
        "x-anx-organization-slug": "local",
      },
    }),
  };
}

function resetEnv() {
  for (const key of Object.keys(mockState.env)) {
    delete mockState.env[key];
  }
}

describe("workspaceResolver", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetEnv();
    clearWorkspaceResolutionCache();
  });

  it("resolves static ANX_WORKSPACES entries", async () => {
    mockState.env.ANX_WORKSPACES =
      '[{"organizationSlug":"local","slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const resolved = await resolveWorkspaceInRoute({
      organizationSlug: "local",
      workspaceSlug: "ops",
    });

    expect(resolved.error).toBeNull();
    expect(resolved.workspace).toMatchObject({
      slug: "ops",
      label: "Ops",
      coreBaseUrl: "http://127.0.0.1:8001",
    });
  });

  it("returns configured catalog with static default workspace", async () => {
    mockState.env.ANX_WORKSPACES =
      '[{"organizationSlug":"local","slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const catalog = await resolveWorkspaceCatalog(createEvent());
    expect(catalog.defaultWorkspace).toMatchObject({
      slug: "ops",
      organizationSlug: "local",
    });
    expect(catalog.workspaceByComposite.has("local:ops")).toBe(true);
  });

  it("returns workspace_not_configured for unknown workspace slug", async () => {
    mockState.env.ANX_WORKSPACES =
      '[{"organizationSlug":"local","slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const resolved = await resolveWorkspaceInRoute({
      organizationSlug: "local",
      workspaceSlug: "missing",
    });

    expect(resolved.workspace).toBeNull();
    expect(resolved.error).toMatchObject({
      status: 404,
      payload: { error: { code: "workspace_not_configured" } },
    });
  });

  it("returns workspace_route_incomplete when org or workspace is empty", async () => {
    mockState.env.ANX_WORKSPACES =
      '[{"organizationSlug":"local","slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const missingOrg = await resolveWorkspaceInRoute({
      organizationSlug: "",
      workspaceSlug: "ops",
    });
    expect(missingOrg.error?.payload?.error?.code).toBe(
      "workspace_route_incomplete",
    );

    const missingWs = await resolveWorkspaceInRoute({
      organizationSlug: "local",
      workspaceSlug: "",
    });
    expect(missingWs.error?.payload?.error?.code).toBe(
      "workspace_route_incomplete",
    );
  });

  it("resolves workspace from control plane when ANX_CONTROL_BASE_URL is configured", async () => {
    mockState.env.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    mockState.env.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN = "tok_dev";

    const fetchMock = vi.fn(async (url) => {
      const u = String(url);
      if (u.includes("/organizations")) {
        return {
          ok: true,
          async json() {
            return {
              organizations: [{ id: "org_test", slug: "local" }],
              next_cursor: "",
            };
          },
        };
      }
      return {
        ok: true,
        async json() {
          return {
            workspaces: [
              {
                id: "ws_test",
                organization_id: "org_test",
                organization_slug: "local",
                slug: "alpha",
                display_name: "Alpha",
                core_origin: "http://127.0.0.1:18001",
                public_origin: "",
              },
            ],
          };
        },
      };
    });

    const resolved = await resolveWorkspaceInRoute({
      organizationSlug: "local",
      workspaceSlug: "alpha",
      event: {
        fetch: fetchMock,
        cookies: { get: () => undefined },
      },
    });

    expect(fetchMock).toHaveBeenCalled();
    expect(resolved.error).toBeNull();
    expect(resolved.workspace).toMatchObject({
      slug: "alpha",
      coreBaseUrl: "http://127.0.0.1:18001",
      id: "ws_test",
      organizationId: "org_test",
      organizationSlug: "local",
    });
  });

  it("returns workspace_not_ready (503) when CP knows the workspace but core_origin is empty", async () => {
    mockState.env.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    mockState.env.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN = "tok_dev";

    const fetchMock = vi.fn(async (url) => {
      const u = String(url);
      if (u.includes("/organizations")) {
        return {
          ok: true,
          async json() {
            return {
              organizations: [{ id: "org_test", slug: "local" }],
              next_cursor: "",
            };
          },
        };
      }
      return {
        ok: true,
        async json() {
          return {
            workspaces: [
              {
                id: "ws_test",
                organization_id: "org_test",
                organization_slug: "local",
                slug: "alpha",
                display_name: "Alpha",
                core_origin: "",
                public_origin: "",
                status: "suspended",
                desired_state: "ready",
              },
            ],
          };
        },
      };
    });

    const resolved = await resolveWorkspaceInRoute({
      organizationSlug: "local",
      workspaceSlug: "alpha",
      event: {
        fetch: fetchMock,
        cookies: { get: () => undefined },
      },
    });

    expect(resolved.workspace).toBeNull();
    expect(resolved.error).toMatchObject({
      status: 503,
      payload: { error: { code: "workspace_not_ready" } },
    });
    expect(resolved.error.payload.error.message).toMatch(/suspended/);
    expect(resolved.error.payload.error.message).toMatch(/retry/i);
  });

  it("hydrates workspace catalog from control plane org list in hosted mode", async () => {
    mockState.env.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    mockState.env.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN = "tok_dev";

    const fetchMock = vi.fn(async (url) => {
      const u = String(url);
      if (u.includes("/organizations")) {
        return {
          ok: true,
          async json() {
            return {
              organizations: [{ id: "org_test", slug: "acme" }],
              next_cursor: "",
            };
          },
        };
      }
      if (u.includes("organization_id=org_test")) {
        return {
          ok: true,
          async json() {
            return {
              workspaces: [
                {
                  id: "ws_a",
                  organization_id: "org_test",
                  organization_slug: "acme",
                  slug: "alpha",
                  display_name: "Alpha",
                  core_origin: "http://127.0.0.1:18001",
                  public_origin: "",
                },
                {
                  id: "ws_b",
                  organization_id: "org_test",
                  organization_slug: "acme",
                  slug: "beta",
                  display_name: "Beta",
                  core_origin: "http://127.0.0.1:18002",
                  public_origin: "",
                },
              ],
            };
          },
        };
      }
      return {
        ok: true,
        async json() {
          return {
            workspaces: [
              {
                id: "ws_a",
                organization_id: "org_test",
                organization_slug: "acme",
                slug: "alpha",
                display_name: "Alpha",
                core_origin: "http://127.0.0.1:18001",
                public_origin: "",
              },
            ],
          };
        },
      };
    });

    const catalog = await resolveWorkspaceCatalog({
      params: { organization: "acme", workspace: "alpha" },
      fetch: fetchMock,
      cookies: { get: () => undefined },
    });

    expect(catalog.workspaces).toHaveLength(2);
    expect(catalog.workspaceByComposite.has("acme:beta")).toBe(true);
    expect(catalog.defaultWorkspace.slug).toBe("alpha");
  });

  it("marks lookup as unauthenticated when hosted provider has no CP token", async () => {
    mockState.env.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    const resolved = await resolveWorkspaceInRoute({
      organizationSlug: "acme",
      workspaceSlug: "alpha",
      event: {
        fetch: vi.fn(),
        cookies: { get: () => undefined },
        locals: {},
      },
    });
    expect(resolved.workspace).toBeNull();
    expect(resolved.outOfWorkspaceUnauthenticated).toBe(true);
    expect(resolved.error?.payload?.error?.code).toBe(
      "workspace_not_configured",
    );
  });

  it("resolves proxy target from static workspace catalog", async () => {
    mockState.env.ANX_WORKSPACES =
      '[{"organizationSlug":"local","slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const resolved = await resolveProxyWorkspaceTarget({
      event: createEvent(),
      workspaceSlug: "ops",
    });

    expect(resolved).toMatchObject({
      workspace: expect.objectContaining({ slug: "ops" }),
      coreBaseUrl: "http://127.0.0.1:8001",
    });
  });

  it("keeps cache APIs as no-op for OSS static resolver", () => {
    clearWorkspaceResolutionCache();
    expect(getWorkspaceResolutionCacheSize()).toBe(0);
  });
});
