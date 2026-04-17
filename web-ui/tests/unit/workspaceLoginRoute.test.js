import { afterEach, describe, expect, it, vi } from "vitest";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceBySlug: vi.fn(),
}));

const authSessionMocks = vi.hoisted(() => ({
  loadWorkspaceAuthenticatedAgent: vi.fn(),
}));

const dynamicPrivateEnv = vi.hoisted(() => ({
  OAR_CONTROL_BASE_URL: "",
}));

vi.mock("$env/dynamic/private", () => ({
  env: dynamicPrivateEnv,
}));

vi.mock("$lib/server/workspaceResolver", () => ({
  resolveWorkspaceBySlug: workspaceResolverMocks.resolveWorkspaceBySlug,
}));

vi.mock("$lib/server/authSession", () => ({
  loadWorkspaceAuthenticatedAgent:
    authSessionMocks.loadWorkspaceAuthenticatedAgent,
}));

import { load } from "../../src/routes/[workspace]/login/+page.server.js";

function createEvent(overrides = {}) {
  return {
    params: {
      workspace: "acme",
    },
    url: new URL("https://ui.example.test/acme/login"),
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
    ...overrides,
  };
}

afterEach(() => {
  dynamicPrivateEnv.OAR_CONTROL_BASE_URL = "";
});

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
      fetch: vi.fn(
        async () =>
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

  it("redirects to launch finish when control-plane session cookie can mint a launch session", async () => {
    dynamicPrivateEnv.OAR_CONTROL_BASE_URL = "https://cp.example.test";
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
      workspaceSlug: "acme",
      workspace: {
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
    authSessionMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const cookieGet = vi.fn((name) =>
      name === "oar_cp_dev_access_token" ? "cp-session-token" : "",
    );

    const event = createEvent({
      fetch: vi.fn(async (url) => {
        const u = String(url);
        if (u.includes("/meta/handshake")) {
          return new Response(
            JSON.stringify({
              human_auth_mode: "external_grant",
            }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          );
        }
        if (u.includes("/workspaces/ws_123/launch-sessions")) {
          expect(u.startsWith("https://cp.example.test/")).toBe(true);
          return new Response(
            JSON.stringify({
              launch_session: {
                finish_url: "/workspaces/ws_123/launch-finish?lid=launch_abc",
              },
            }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          );
        }
        return new Response("unexpected url", { status: 500 });
      }),
      cookies: { get: cookieGet },
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 303,
      location: "/hosted/api/workspaces/ws_123/launch-finish?lid=launch_abc",
    });
  });

  it("falls through to hosted sign-in when launch-sessions returns 401", async () => {
    dynamicPrivateEnv.OAR_CONTROL_BASE_URL = "https://cp.example.test";
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
        "https://ui.example.test/acme/login?return_to=%2Fthreads%2F1",
      ),
      fetch: vi.fn(async (url) => {
        const u = String(url);
        if (u.includes("/meta/handshake")) {
          return new Response(
            JSON.stringify({
              human_auth_mode: "external_grant",
            }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          );
        }
        if (u.includes("/launch-sessions")) {
          return new Response(
            JSON.stringify({ error: { code: "unauthorized" } }),
            { status: 401, headers: { "content-type": "application/json" } },
          );
        }
        return new Response("unexpected", { status: 500 });
      }),
      cookies: {
        get: vi.fn((name) =>
          name === "oar_cp_dev_access_token" ? "stale-token" : "",
        ),
      },
    });

    await expect(load(event)).rejects.toMatchObject({
      status: 307,
      location:
        "/hosted/signin?workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F1",
    });
  });
});
