import { describe, expect, it, vi } from "vitest";

import {
  mockHostedProvider,
  mockLocalProvider,
} from "../fixtures/workspaceAuth.js";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
}));

const authSessionMocks = vi.hoisted(() => ({
  loadWorkspaceAuthenticatedAgent: vi.fn(),
}));

vi.mock("$lib/server/workspaceResolver", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
  };
});

vi.mock("$lib/server/authSession", () => ({
  loadWorkspaceAuthenticatedAgent:
    authSessionMocks.loadWorkspaceAuthenticatedAgent,
}));

import { load } from "../../src/routes/o/[organization]/w/[workspace]/login/+page.server.js";

function createEvent(overrides = {}) {
  return {
    params: {
      organization: "local",
      workspace: "acme",
    },
    url: new URL("https://ui.example.test/o/local/w/acme/login"),
    cookies: {
      get: vi.fn(() => ""),
    },
    fetch: vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            human_auth_mode: "workspace_local",
          }),
          {
            status: 200,
            headers: {
              "content-type": "application/json",
            },
          },
        ),
    ),
    locals: {
      outOfWorkspace: mockLocalProvider(),
    },
    ...overrides,
  };
}

describe("workspace login route", () => {
  it("redirects to hosted sign-in for external grant mode when unauthenticated", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      url: new URL(
        "https://ui.example.test/o/local/w/acme/login?return_to=%2Fthreads%2F123%3Ftab%3Dnotes",
      ),
      fetch: vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              human_auth_mode: "external_grant",
            }),
            {
              status: 200,
              headers: {
                "content-type": "application/json",
              },
            },
          ),
      ),
      locals: {
        outOfWorkspace: mockHostedProvider({
          beginLaunchSession: vi.fn(async () => ({
            kind: "needs_signin",
            signInUrl:
              "/hosted/signin?workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F123%3Ftab%3Dnotes",
          })),
        }),
      },
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location:
        "/hosted/signin?workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F123%3Ftab%3Dnotes",
    });
  });

  it("keeps workspace-local login flow when auth mode is not external_grant", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent();
    await expect(load(event)).resolves.toBeUndefined();
  });

  it("does not render workspace-local login when hosted provider is active", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      fetch: vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              human_auth_mode: "workspace_local",
            }),
            {
              status: 200,
              headers: {
                "content-type": "application/json",
              },
            },
          ),
      ),
      locals: {
        outOfWorkspace: mockHostedProvider({
          beginLaunchSession: vi.fn(async () => ({
            kind: "needs_signin",
            signInUrl: "/hosted/signin?workspace=acme&workspace_id=ws_123",
          })),
        }),
      },
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location: "/hosted/signin?workspace=acme&workspace_id=ws_123",
    });
  });

  it("fails closed to hosted sign-in when hosted handshake lookup fails", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      fetch: vi.fn(async () => {
        throw new Error("network timeout");
      }),
      locals: {
        outOfWorkspace: mockHostedProvider({
          beginLaunchSession: vi.fn(async () => ({
            kind: "needs_signin",
            signInUrl: "/hosted/signin?workspace=acme&workspace_id=ws_123",
          })),
        }),
      },
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location: "/hosted/signin?workspace=acme&workspace_id=ws_123",
    });
  });

  it("fails closed to hosted sign-in when hosted handshake omits auth mode", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      fetch: vi.fn(
        async () =>
          new Response(JSON.stringify({ dev_actor_mode: false }), {
            status: 200,
            headers: {
              "content-type": "application/json",
            },
          }),
      ),
      locals: {
        outOfWorkspace: mockHostedProvider({
          beginLaunchSession: vi.fn(async () => ({
            kind: "needs_signin",
            signInUrl: "/hosted/signin?workspace=acme&workspace_id=ws_123",
          })),
        }),
      },
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location: "/hosted/signin?workspace=acme&workspace_id=ws_123",
    });
  });

  it("keeps workspace-local flow for non-hosted contexts when handshake lookup fails", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "local",
      workspace: {
        coreBaseUrl: "https://core.example.test",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      params: {
        organization: "local",
        workspace: "local",
      },
      url: new URL("https://ui.example.test/o/local/w/local/login"),
      fetch: vi.fn(async () => {
        throw new Error("network timeout");
      }),
      locals: {
        outOfWorkspace: mockLocalProvider(),
      },
    });

    await expect(load(event)).resolves.toBeUndefined();
  });

  it("redirects authenticated users to workspace home", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue({
      agent_id: "agent_123",
    });

    const event = createEvent();
    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location: "/o/local/w/acme",
    });
  });

  it("redirects to launch finish when provider returns redirect launch instruction", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      fetch: vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              human_auth_mode: "external_grant",
            }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          ),
      ),
      locals: {
        outOfWorkspace: mockHostedProvider({
          beginLaunchSession: vi.fn(async () => ({
            kind: "redirect",
            finishUrl:
              "/hosted/api/workspaces/ws_123/launch-finish?lid=launch_abc",
          })),
        }),
      },
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 303,
      location: "/hosted/api/workspaces/ws_123/launch-finish?lid=launch_abc",
    });
  });

  it("falls through to hosted sign-in when provider returns needs_signin", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      url: new URL(
        "https://ui.example.test/o/local/w/acme/login?return_to=%2Fthreads%2F1",
      ),
      fetch: vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              human_auth_mode: "external_grant",
            }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          ),
      ),
      locals: {
        outOfWorkspace: mockHostedProvider({
          beginLaunchSession: vi.fn(async () => ({
            kind: "needs_signin",
            signInUrl:
              "/hosted/signin?workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F1",
          })),
        }),
      },
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location:
        "/hosted/signin?workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F1",
    });
  });
});
