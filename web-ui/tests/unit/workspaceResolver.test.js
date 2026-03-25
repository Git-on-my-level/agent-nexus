import { beforeEach, describe, expect, it, vi } from "vitest";

const mockState = vi.hoisted(() => ({
  env: {},
  accessToken: "",
  listWorkspaces: vi.fn(),
  clearControlAccessToken: vi.fn(),
  clearControlAccount: vi.fn(),
}));

vi.mock("$env/dynamic/private", () => ({
  env: mockState.env,
}));

vi.mock("../../src/lib/server/controlClient.js", () => ({
  createControlClient: vi.fn(() => ({
    listWorkspaces: mockState.listWorkspaces,
  })),
}));

vi.mock("../../src/lib/server/controlSession.js", () => ({
  readControlAccessToken: vi.fn(() => mockState.accessToken),
  clearControlAccessToken: mockState.clearControlAccessToken,
  clearControlAccount: mockState.clearControlAccount,
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
    cookies: {
      get() {
        return null;
      },
      set() {},
      delete() {},
    },
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
    mockState.accessToken = "";
    clearWorkspaceResolutionCache();
  });

  it("falls back to static OAR_WORKSPACES without contacting the control plane", async () => {
    mockState.env.OAR_WORKSPACES =
      '[{"slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}]';

    const resolved = await resolveWorkspaceBySlug({
      event: createEvent(),
      workspaceSlug: "ops",
    });

    expect(resolved.error).toBeNull();
    expect(resolved.workspace).toMatchObject({
      slug: "ops",
      label: "Ops",
      coreBaseUrl: "http://127.0.0.1:8001",
    });
    expect(mockState.listWorkspaces).not.toHaveBeenCalled();
  });

  it("resolves dynamic SaaS workspaces from the control plane and caches them briefly", async () => {
    mockState.accessToken = "control-token";
    mockState.listWorkspaces.mockResolvedValue({
      workspaces: [
        {
          workspace_id: "ws-123",
          organization_id: "org-1",
          slug: "alpha",
          display_name: "Alpha",
          workspace_path: "/alpha",
          public_origin: "https://app.example.test/alpha",
          core_origin: "https://alpha-core.example.test/",
        },
      ],
    });

    const first = await resolveWorkspaceBySlug({
      event: createEvent(),
      workspaceSlug: "alpha",
    });
    const second = await resolveProxyWorkspaceTarget({
      event: createEvent(),
      workspaceSlug: "alpha",
    });

    expect(first.error).toBeNull();
    expect(first.workspace).toMatchObject({
      slug: "alpha",
      label: "Alpha",
      coreBaseUrl: "https://alpha-core.example.test",
      workspaceId: "ws-123",
      source: "control-plane",
    });
    expect(first.catalog.workspaces).toEqual([
      expect.objectContaining({ slug: "alpha" }),
    ]);
    expect(second).toMatchObject({
      workspace: {
        slug: "alpha",
      },
      coreBaseUrl: "https://alpha-core.example.test",
    });
    expect(mockState.listWorkspaces).toHaveBeenCalledTimes(1);
  });

  it("uses the control-plane catalog as the default workspace list when static config is synthetic", async () => {
    mockState.accessToken = "control-token";
    mockState.listWorkspaces.mockResolvedValue({
      workspaces: [
        {
          workspace_id: "ws-123",
          organization_id: "org-1",
          slug: "alpha",
          display_name: "Alpha",
          workspace_path: "/alpha",
          public_origin: "https://app.example.test/alpha",
          core_origin: "https://alpha-core.example.test/",
        },
        {
          workspace_id: "ws-456",
          organization_id: "org-1",
          slug: "beta",
          display_name: "Beta",
          workspace_path: "/beta",
          public_origin: "https://app.example.test/beta",
          core_origin: "https://beta-core.example.test/",
        },
      ],
    });

    const catalog = await resolveWorkspaceCatalog(createEvent());

    expect(catalog.defaultWorkspace).toMatchObject({
      slug: "alpha",
      source: "control-plane",
    });
    expect(catalog.workspaces).toEqual([
      expect.objectContaining({ slug: "alpha", source: "control-plane" }),
      expect.objectContaining({ slug: "beta", source: "control-plane" }),
    ]);
    expect(catalog.workspaceBySlug.has("local")).toBe(false);
  });

  it("returns an operator-friendly 404 when a control-plane workspace is missing or revoked", async () => {
    mockState.accessToken = "control-token";
    mockState.listWorkspaces.mockResolvedValue({
      workspaces: [],
    });

    const resolved = await resolveWorkspaceBySlug({
      event: createEvent(),
      workspaceSlug: "missing",
    });

    expect(resolved.workspace).toBeNull();
    expect(resolved.error).toMatchObject({
      status: 404,
      payload: {
        error: {
          code: "workspace_unavailable",
        },
      },
    });
    expect(resolved.error.payload.error.message).toContain(
      "control-plane workspace catalog",
    );
  });

  it("keeps invalid provided slugs invalid instead of resolving the default workspace", async () => {
    mockState.env.OAR_WORKSPACES =
      '[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]';

    const resolved = await resolveWorkspaceBySlug({
      event: createEvent(),
      workspaceSlug: "@@@",
    });

    expect(resolved.workspace).toBeNull();
    expect(resolved.workspaceSlug).toBe("@@@");
    expect(resolved.error).toMatchObject({
      status: 404,
      payload: {
        error: {
          code: "workspace_not_configured",
        },
      },
    });
    expect(mockState.listWorkspaces).not.toHaveBeenCalled();
  });

  it("prunes expired control-plane workspace cache entries as tokens rotate", async () => {
    const nowSpy = vi.spyOn(Date, "now");
    nowSpy.mockReturnValue(0);

    mockState.accessToken = "control-token-a";
    mockState.listWorkspaces.mockResolvedValueOnce({
      workspaces: [
        {
          workspace_id: "ws-123",
          organization_id: "org-1",
          slug: "alpha",
          display_name: "Alpha",
          workspace_path: "/alpha",
          public_origin: "https://app.example.test/alpha",
          core_origin: "https://alpha-core.example.test/",
        },
      ],
    });

    await resolveWorkspaceBySlug({
      event: createEvent(),
      workspaceSlug: "alpha",
    });
    expect(getWorkspaceResolutionCacheSize()).toBe(1);

    nowSpy.mockReturnValue(6000);
    mockState.accessToken = "control-token-b";
    mockState.listWorkspaces.mockResolvedValueOnce({
      workspaces: [
        {
          workspace_id: "ws-456",
          organization_id: "org-1",
          slug: "beta",
          display_name: "Beta",
          workspace_path: "/beta",
          public_origin: "https://app.example.test/beta",
          core_origin: "https://beta-core.example.test/",
        },
      ],
    });

    await resolveWorkspaceBySlug({
      event: createEvent(),
      workspaceSlug: "beta",
    });

    expect(getWorkspaceResolutionCacheSize()).toBe(1);
    expect(mockState.listWorkspaces).toHaveBeenCalledTimes(2);
    nowSpy.mockRestore();
  });

  it("clears stale control auth and reports an explicit session error", async () => {
    mockState.accessToken = "expired-token";
    mockState.listWorkspaces.mockRejectedValue(
      Object.assign(new Error("unauthorized"), { status: 401 }),
    );

    const resolved = await resolveWorkspaceBySlug({
      event: createEvent(),
      workspaceSlug: "alpha",
    });

    expect(resolved.error).toMatchObject({
      status: 401,
      payload: {
        error: {
          code: "control_session_required",
        },
      },
    });
    expect(mockState.clearControlAccessToken).toHaveBeenCalledTimes(1);
    expect(mockState.clearControlAccount).toHaveBeenCalledTimes(1);
  });
});
