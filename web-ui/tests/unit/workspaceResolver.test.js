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

  it("derives proxy workspace target from same-origin referer when headers are missing", async () => {
    mockState.env.ANX_WORKSPACES =
      '[{"organizationSlug":"local","slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const target = await resolveProxyWorkspaceTarget({
      workspaceSlug: "",
      event: {
        url: new URL("https://anx.example.test/api/threads"),
        request: new Request("https://anx.example.test/api/threads", {
          headers: {
            referer: "https://anx.example.test/o/local/w/ops/topics",
          },
        }),
      },
    });

    expect(target.status).toBeUndefined();
    expect(target.workspace).toMatchObject({
      organizationSlug: "local",
      slug: "ops",
    });
    expect(target.coreBaseUrl).toBe("http://127.0.0.1:8001");
  });

  it("does not trust cross-origin referers for proxy workspace fallback", async () => {
    const target = await resolveProxyWorkspaceTarget({
      workspaceSlug: "",
      event: {
        url: new URL("https://anx.example.test/api/threads"),
        request: new Request("https://anx.example.test/api/threads", {
          headers: {
            referer: "https://evil.example.test/o/local/w/ops/topics",
          },
        }),
      },
    });

    expect(target).toMatchObject({
      status: 400,
      payload: {
        error: {
          code: "workspace_header_required",
        },
      },
    });
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
