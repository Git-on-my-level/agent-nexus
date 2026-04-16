import { describe, expect, it, vi } from "vitest";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceBySlug: vi.fn(),
}));

const authSessionMocks = vi.hoisted(() => ({
  loadWorkspaceAuthenticatedAgent: vi.fn(),
}));

vi.mock("$lib/server/workspaceResolver", () => ({
  resolveWorkspaceBySlug: workspaceResolverMocks.resolveWorkspaceBySlug,
}));

vi.mock("$lib/server/authSession", () => ({
  loadWorkspaceAuthenticatedAgent: authSessionMocks.loadWorkspaceAuthenticatedAgent,
}));

import { load } from "../../src/routes/[workspace]/login/+page.server.js";

function createEvent(overrides = {}) {
  return {
    params: {
      workspace: "acme",
    },
    url: new URL("https://ui.example.test/acme/login"),
    fetch: vi.fn(async () =>
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
    ...overrides,
  };
}

describe("workspace login route", () => {
  it("redirects to hosted sign-in for external grant mode when unauthenticated", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
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
        "https://ui.example.test/acme/login?return_to=%2Fthreads%2F123%3Ftab%3Dnotes",
      ),
      fetch: vi.fn(async () =>
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
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location:
        "/hosted/signin?workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F123%3Ftab%3Dnotes",
    });
  });

  it("keeps workspace-local login flow when auth mode is not external_grant", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
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

  it("fails closed to hosted sign-in when hosted handshake lookup fails", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
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
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location: "/hosted/signin?workspace=acme&workspace_id=ws_123",
    });
  });

  it("fails closed to hosted sign-in when hosted handshake omits auth mode", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      fetch: vi.fn(async () =>
        new Response(JSON.stringify({ dev_actor_mode: false }), {
          status: 200,
          headers: {
            "content-type": "application/json",
          },
        }),
      ),
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location: "/hosted/signin?workspace=acme&workspace_id=ws_123",
    });
  });

  it("keeps workspace-local flow for non-hosted contexts when handshake lookup fails", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
      workspaceSlug: "local",
      workspace: {
        coreBaseUrl: "https://core.example.test",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const event = createEvent({
      params: {
        workspace: "local",
      },
      fetch: vi.fn(async () => {
        throw new Error("network timeout");
      }),
    });

    await expect(load(event)).resolves.toBeUndefined();
  });

  it("redirects authenticated users to workspace home", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
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
      location: "/acme",
    });
  });
});
