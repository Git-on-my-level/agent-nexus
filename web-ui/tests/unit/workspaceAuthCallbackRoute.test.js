import { beforeEach, describe, expect, it, vi } from "vitest";

const envState = vi.hoisted(() => ({
  ANX_CONTROL_BASE_URL: "https://control.example.test",
}));

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceBySlug: vi.fn(),
}));

const authSessionMocks = vi.hoisted(() => ({
  clearRetryableWorkspaceAuthFailureCount: vi.fn(),
  writeWorkspaceAccessToken: vi.fn(),
  writeWorkspaceRefreshToken: vi.fn(),
}));

vi.mock("$env/dynamic/private", () => ({
  env: envState,
}));

vi.mock("$lib/server/workspaceResolver.js", () => ({
  resolveWorkspaceBySlug: workspaceResolverMocks.resolveWorkspaceBySlug,
}));

vi.mock("$lib/server/authSession.js", () => ({
  clearRetryableWorkspaceAuthFailureCount:
    authSessionMocks.clearRetryableWorkspaceAuthFailureCount,
  writeWorkspaceAccessToken: authSessionMocks.writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken: authSessionMocks.writeWorkspaceRefreshToken,
}));

import { POST } from "../../src/routes/[workspace]/auth/callback/+server.js";

function createEvent(formFields, options = {}) {
  const { headers: headerOverrides = {}, ...rest } = options;
  return {
    params: {
      workspace: "acme",
    },
    request: new Request("https://ui.example.test/acme/auth/callback", {
      method: "POST",
      body: new URLSearchParams(formFields),
      headers: {
        accept: "application/json",
        ...headerOverrides,
      },
      ...rest,
    }),
    cookies: {
      set: vi.fn(),
      delete: vi.fn(),
      get: vi.fn(),
    },
  };
}

describe("workspace auth callback route", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
      workspaceSlug: "acme",
      workspace: {
        slug: "acme",
        label: "Acme",
        coreBaseUrl: "https://core.example.test",
        workspaceId: "ws_123",
      },
      error: null,
    });
  });

  it("exchanges launch session and workspace grant, then redirects with auth cookies", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            grant: { bearer_token: "grant_token" },
          }),
          {
            status: 200,
            headers: {
              "content-type": "application/json",
            },
          },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            tokens: {
              access_token: "access_123",
              refresh_token: "refresh_123",
            },
          }),
          {
            status: 200,
            headers: {
              "content-type": "application/json",
            },
          },
        ),
      );
    globalThis.fetch = fetchMock;

    const event = createEvent({
      exchange_token: "ex_123",
      state: "state_123",
      workspace_id: "ws_123",
      return_path: "/threads?view=mine",
    });

    await expect(POST(event)).rejects.toMatchObject({
      status: 303,
      location: "/acme/threads?view=mine",
    });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0][0]).toBe(
      "https://control.example.test/workspaces/ws_123/session-exchange",
    );
    expect(fetchMock.mock.calls[1][0]).toBe(
      "https://core.example.test/auth/token",
    );
    expect(authSessionMocks.writeWorkspaceRefreshToken).toHaveBeenCalledWith(
      event,
      "acme",
      "refresh_123",
    );
    expect(authSessionMocks.writeWorkspaceAccessToken).toHaveBeenCalledWith(
      event,
      "acme",
      "access_123",
    );
    expect(
      authSessionMocks.clearRetryableWorkspaceAuthFailureCount,
    ).toHaveBeenCalledWith(event, "acme");
  });

  it("rejects invalid callback payloads", async () => {
    const event = createEvent({
      exchange_token: "",
      state: "",
    });

    const response = await POST(event);
    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toMatchObject({
      error: {
        code: "invalid_request",
      },
    });
  });

  it("returns upstream control-plane failures", async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: {
            code: "exchange_invalid",
            message: "exchange token has already been used",
          },
        }),
        {
          status: 409,
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    );

    const event = createEvent({
      exchange_token: "ex_123",
      state: "state_123",
      workspace_id: "ws_123",
    });

    const response = await POST(event);
    expect(response.status).toBe(409);
    await expect(response.json()).resolves.toMatchObject({
      error: {
        code: "exchange_invalid",
        message: "exchange token has already been used",
        workspace_name: "Acme",
      },
    });
  });

  it("normalizes control-plane state mismatch to state_mismatch", async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: {
            code: "exchange_invalid",
            message: "exchange token state is invalid",
          },
        }),
        {
          status: 401,
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    );

    const event = createEvent({
      exchange_token: "ex_123",
      state: "state_123",
      workspace_id: "ws_123",
    });

    const response = await POST(event);
    expect(response.status).toBe(401);
    await expect(response.json()).resolves.toMatchObject({
      error: {
        code: "state_mismatch",
        message: "exchange token state is invalid",
      },
    });
  });

  it("returns workspace token exchange failures from core", async () => {
    globalThis.fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            grant: { bearer_token: "grant_token" },
          }),
          {
            status: 200,
            headers: {
              "content-type": "application/json",
            },
          },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            error: {
              code: "invalid_token",
              message: "workspace grant assertion could not be validated",
            },
          }),
          {
            status: 401,
            headers: {
              "content-type": "application/json",
            },
          },
        ),
      );

    const event = createEvent({
      exchange_token: "ex_123",
      state: "state_123",
      workspace_id: "ws_123",
      return_path: "https://evil.test",
    });

    const response = await POST(event);
    expect(response.status).toBe(401);
    await expect(response.json()).resolves.toMatchObject({
      error: {
        code: "invalid_token",
      },
    });
  });
});
