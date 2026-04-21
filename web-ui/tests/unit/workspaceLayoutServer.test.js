import { beforeEach, describe, expect, it, vi } from "vitest";

const envState = vi.hoisted(() => ({}));

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
  resolveWorkspaceCatalog: vi.fn(),
}));

const controlPlaneWorkspaceMocks = vi.hoisted(() => ({
  isHostedWebUiShell: vi.fn(),
}));

vi.mock("$env/dynamic/private", () => ({
  env: envState,
}));

vi.mock("$lib/server/workspaceResolver", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
    resolveWorkspaceCatalog: workspaceResolverMocks.resolveWorkspaceCatalog,
  };
});

vi.mock("$lib/server/controlPlaneWorkspace", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    isHostedWebUiShell: controlPlaneWorkspaceMocks.isHostedWebUiShell,
  };
});

import { load } from "../../src/routes/o/[organization]/w/[workspace]/+layout.server.js";

function createEvent({
  pathname = "/o/my-org/w/my-ws/login",
  cookies = {},
} = {}) {
  return {
    params: { organization: "my-org", workspace: "my-ws" },
    url: new URL(`https://ui.example.test${pathname}`),
    cookies: {
      get: vi.fn((name) => cookies[name]),
      set: vi.fn(),
      delete: vi.fn(),
    },
  };
}

function resetEnv(overrides = {}) {
  for (const key of Object.keys(envState)) {
    delete envState[key];
  }
  Object.assign(envState, overrides);
}

describe("workspace +layout.server.js", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetEnv();
    controlPlaneWorkspaceMocks.isHostedWebUiShell.mockReturnValue(false);
    workspaceResolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      workspaces: [],
      defaultWorkspaceSlug: null,
      defaultOrganizationSlug: null,
    });
  });

  it("redirects to /hosted/signin in hosted mode when workspace resolution fails and no CP token is present", async () => {
    controlPlaneWorkspaceMocks.isHostedWebUiShell.mockReturnValue(true);
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: { status: 404, payload: { error: { message: "missing" } } },
    });

    const event = createEvent({ pathname: "/o/my-org/w/my-ws/login" });

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown).toBeDefined();
    expect(thrown.status).toBe(307);
    expect(thrown.location).toContain("/hosted/signin");
    expect(thrown.location).toContain("workspace=my-ws");
    expect(thrown.location).toContain(
      `return_path=${encodeURIComponent("/o/my-org/w/my-ws/login")}`,
    );
  });

  it("does NOT redirect when a CP access token cookie is present (real failure)", async () => {
    controlPlaneWorkspaceMocks.isHostedWebUiShell.mockReturnValue(true);
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: { status: 404, payload: { error: { message: "boom" } } },
    });

    const event = createEvent({
      cookies: { oar_cp_dev_access_token: "tok-123" },
    });

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown).toBeDefined();
    expect(thrown.status).toBe(404);
    expect(thrown.location).toBeUndefined();
    expect(thrown.body?.message ?? thrown.message).toContain("boom");
  });

  it("does NOT redirect in non-hosted mode (regression: keep 404 behavior)", async () => {
    controlPlaneWorkspaceMocks.isHostedWebUiShell.mockReturnValue(false);
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: { status: 404, payload: { error: { message: "missing" } } },
    });

    const event = createEvent();

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown.status).toBe(404);
    expect(thrown.location).toBeUndefined();
  });

  it("does NOT redirect when ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN is set in env", async () => {
    controlPlaneWorkspaceMocks.isHostedWebUiShell.mockReturnValue(true);
    resetEnv({ ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "env-tok" });
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: { status: 404, payload: { error: { message: "missing" } } },
    });

    const event = createEvent();

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown.status).toBe(404);
    expect(thrown.location).toBeUndefined();
  });

  it("does NOT redirect to signin for workspace_not_ready (503) — surfaces the 503 instead", async () => {
    controlPlaneWorkspaceMocks.isHostedWebUiShell.mockReturnValue(true);
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: {
        status: 503,
        payload: {
          error: {
            code: "workspace_not_ready",
            message: "Workspace 'my-org/my-ws' is not ready yet.",
          },
        },
      },
    });

    const event = createEvent();

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown).toBeDefined();
    expect(thrown.status).toBe(503);
    expect(thrown.location).toBeUndefined();
    expect(thrown.body?.message ?? thrown.message).toMatch(/not ready/);
    // The structured `code` must be on the thrown body so $page.error.code
    // is populated and +error.svelte renders the friendly copy instead of
    // the generic "Internal Error".
    expect(thrown.body?.code).toBe("workspace_not_ready");
  });

  it("returns workspace data on successful resolution", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      workspace: {
        organizationSlug: "my-org",
        slug: "my-ws",
        label: "My WS",
        description: "",
        coreBaseUrl: "https://core.example.test",
      },
    });

    const event = createEvent();
    const result = await load(event);

    expect(result.workspace.slug).toBe("my-ws");
    expect(event.cookies.set).toHaveBeenCalledWith(
      expect.any(String),
      expect.stringContaining("my-org"),
      expect.objectContaining({ path: "/", httpOnly: true }),
    );
  });
});
