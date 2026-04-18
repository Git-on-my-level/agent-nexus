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
  resolveWorkspaceBySlug,
} from "../../src/lib/server/workspaceResolver.js";

function createEvent() {
  return {
    params: {},
    request: new Request("https://oar.example.test/api/threads"),
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
      '[{"slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const resolved = await resolveWorkspaceBySlug({
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
      '[{"slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const catalog = await resolveWorkspaceCatalog(createEvent());
    expect(catalog.defaultWorkspace).toMatchObject({ slug: "ops" });
    expect(catalog.workspaceBySlug.has("ops")).toBe(true);
  });

  it("returns workspace_not_configured for unknown workspace slug", async () => {
    mockState.env.ANX_WORKSPACES =
      '[{"slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const resolved = await resolveWorkspaceBySlug({
      workspaceSlug: "missing",
    });

    expect(resolved.workspace).toBeNull();
    expect(resolved.error).toMatchObject({
      status: 404,
      payload: { error: { code: "workspace_not_configured" } },
    });
  });

  it("resolves workspace from control plane when SaaS packed-host dev is enabled", async () => {
    mockState.env.ANX_SAAS_PACKED_HOST_DEV = "1";
    mockState.env.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    mockState.env.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN = "tok_dev";

    const fetchMock = vi.fn(async () => ({
      ok: true,
      async json() {
        return {
          workspaces: [
            {
              id: "ws_test",
              organization_id: "org_test",
              slug: "alpha",
              display_name: "Alpha",
              core_origin: "http://127.0.0.1:18001",
              public_origin: "",
            },
          ],
        };
      },
    }));

    const resolved = await resolveWorkspaceBySlug({
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
    });
  });

  it("hydrates workspace catalog from control plane org list in SaaS packed-host mode", async () => {
    mockState.env.ANX_SAAS_PACKED_HOST_DEV = "1";
    mockState.env.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    mockState.env.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN = "tok_dev";

    const fetchMock = vi.fn(async (url) => {
      const u = String(url);
      if (u.includes("organization_id=org_test")) {
        return {
          ok: true,
          async json() {
            return {
              workspaces: [
                {
                  id: "ws_a",
                  organization_id: "org_test",
                  slug: "alpha",
                  display_name: "Alpha",
                  core_origin: "http://127.0.0.1:18001",
                  public_origin: "",
                },
                {
                  id: "ws_b",
                  organization_id: "org_test",
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
      params: { workspace: "alpha" },
      fetch: fetchMock,
      cookies: { get: () => undefined },
    });

    expect(catalog.workspaces).toHaveLength(2);
    expect(catalog.workspaceBySlug.has("beta")).toBe(true);
    expect(catalog.defaultWorkspace.slug).toBe("alpha");
  });

  it("resolves proxy target from static workspace catalog", async () => {
    mockState.env.ANX_WORKSPACES =
      '[{"slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

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
