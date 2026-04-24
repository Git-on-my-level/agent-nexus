import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  mockHostedProvider,
  mockLocalProvider,
} from "../fixtures/workspaceAuth.js";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
  resolveWorkspaceCatalog: vi.fn(),
}));

vi.mock("$lib/server/workspaceResolver", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
    resolveWorkspaceCatalog: workspaceResolverMocks.resolveWorkspaceCatalog,
  };
});

import { load } from "../../src/routes/o/[organization]/w/[workspace]/+layout.server.js";

function createEvent({
  pathname = "/o/my-org/w/my-ws/login",
  cookies = {},
  outOfWorkspace = mockLocalProvider(),
} = {}) {
  return {
    params: { organization: "my-org", workspace: "my-ws" },
    url: new URL(`https://ui.example.test${pathname}`),
    cookies: {
      get: vi.fn((name) => cookies[name]),
      set: vi.fn(),
      delete: vi.fn(),
    },
    locals: {
      outOfWorkspace,
    },
  };
}

describe("workspace +layout.server.js", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceResolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      workspaces: [],
      defaultWorkspaceSlug: null,
      defaultOrganizationSlug: null,
    });
  });

  it("redirects to /hosted/signin when hosted provider is unauthenticated for workspace lookup", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: {
        status: 404,
        payload: {
          error: { code: "workspace_not_configured", message: "missing" },
        },
      },
      outOfWorkspaceUnauthenticated: true,
    });
    const hosted = mockHostedProvider({
      buildSignInUrl: vi.fn(() => "/hosted/signin?workspace=my-ws"),
    });
    const event = createEvent({ outOfWorkspace: hosted });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location: "/hosted/signin?workspace=my-ws",
    });
  });

  it("does not redirect to signin when hosted provider resolved lookup but workspace is missing", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: {
        status: 404,
        payload: {
          error: { code: "workspace_not_configured", message: "boom" },
        },
      },
      outOfWorkspaceUnauthenticated: false,
    });
    const event = createEvent({ outOfWorkspace: mockHostedProvider() });

    await expect(load(event)).rejects.toMatchObject({
      status: 404,
    });
  });

  it("does not redirect in local provider mode", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: {
        status: 404,
        payload: {
          error: { code: "workspace_not_configured", message: "missing" },
        },
      },
      outOfWorkspaceUnauthenticated: false,
    });
    const event = createEvent();

    await expect(load(event)).rejects.toMatchObject({
      status: 404,
    });
  });

  it("does not redirect for workspace_not_ready", async () => {
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
      outOfWorkspaceUnauthenticated: false,
    });
    const event = createEvent({ outOfWorkspace: mockHostedProvider() });

    await expect(load(event)).rejects.toMatchObject({
      status: 503,
      body: expect.objectContaining({ code: "workspace_not_ready" }),
    });
  });

  it("returns workspace data on successful resolution", async () => {
    const resolvedWorkspace = {
      error: null,
      outOfWorkspaceUnauthenticated: false,
      workspace: {
        organizationSlug: "my-org",
        slug: "my-ws",
        label: "My WS",
        description: "",
        coreBaseUrl: "https://core.example.test",
      },
    };
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue(
      resolvedWorkspace,
    );

    const event = createEvent();
    const result = await load(event);

    expect(result.workspace.slug).toBe("my-ws");
    expect(workspaceResolverMocks.resolveWorkspaceCatalog).toHaveBeenCalledWith(
      event,
      { prefetchedResolved: resolvedWorkspace },
    );
    expect(event.cookies.set).toHaveBeenCalledWith(
      expect.any(String),
      expect.stringContaining("my-org"),
      expect.objectContaining({ path: "/", httpOnly: true }),
    );
  });

  it("runs silent hosted launch bridge when no workspace auth cookies are present", async () => {
    const beginLaunchSession = vi.fn(async () => ({
      kind: "redirect",
      finishUrl: "/hosted/api/workspaces/ws-1/launch-finish?lid=abc",
    }));
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      outOfWorkspaceUnauthenticated: false,
      workspace: {
        organizationSlug: "my-org",
        slug: "my-ws",
        label: "My WS",
        description: "",
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws-1",
      },
    });
    const event = createEvent({
      outOfWorkspace: mockHostedProvider({
        beginLaunchSession,
      }),
      cookies: {},
      pathname: "/o/my-org/w/my-ws/threads/123?tab=notes",
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 303,
      location: "/hosted/api/workspaces/ws-1/launch-finish?lid=abc",
    });
    expect(beginLaunchSession).toHaveBeenCalledWith(
      expect.objectContaining({
        workspaceId: "ws-1",
        returnPath: "/threads/123?tab=notes",
      }),
    );
  });
});
