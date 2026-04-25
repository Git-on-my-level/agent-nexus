import { beforeEach, describe, expect, it, vi } from "vitest";

import { mockHostedProvider } from "../fixtures/workspaceAuth.js";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
}));

const authSessionMocks = vi.hoisted(() => ({
  clearRetryableWorkspaceAuthFailureCount: vi.fn(),
  writeWorkspaceAccessToken: vi.fn(),
  writeWorkspaceRefreshToken: vi.fn(),
}));

vi.mock("$lib/server/workspaceResolver.js", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
  };
});

vi.mock("$lib/server/authSession.js", () => ({
  clearRetryableWorkspaceAuthFailureCount:
    authSessionMocks.clearRetryableWorkspaceAuthFailureCount,
  writeWorkspaceAccessToken: authSessionMocks.writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken: authSessionMocks.writeWorkspaceRefreshToken,
}));

import {
  GET,
  POST,
} from "../../src/routes/o/[organization]/w/[workspace]/auth/callback/+server.js";

function createEvent(formFields, options = {}) {
  const {
    headers: headerOverrides = {},
    outOfWorkspace = mockHostedProvider({
      exchangeLaunchSession: vi.fn(async () => ({
        ok: true,
        assertion: "grant_token",
      })),
    }),
    ...rest
  } = options;
  return {
    params: {
      organization: "local",
      workspace: "acme",
    },
    request: new Request(
      "https://ui.example.test/o/local/w/acme/auth/callback",
      {
        method: "POST",
        body: new URLSearchParams(formFields),
        headers: {
          accept: "application/json",
          ...headerOverrides,
        },
        ...rest,
      },
    ),
    locals: {
      outOfWorkspace,
    },
    cookies: {
      set: vi.fn(),
      delete: vi.fn(),
      get: vi.fn(),
    },
  };
}

function createGetEvent(queryFields, options = {}) {
  const {
    headers: headerOverrides = {},
    outOfWorkspace = mockHostedProvider({
      exchangeLaunchSession: vi.fn(async () => ({
        ok: true,
        assertion: "grant_token",
      })),
    }),
  } = options;
  const query = new URLSearchParams(queryFields).toString();
  const url = `https://ui.example.test/o/local/w/acme/auth/callback${query ? `?${query}` : ""}`;
  return {
    params: {
      organization: "local",
      workspace: "acme",
    },
    url: new URL(url),
    request: new Request(url, {
      method: "GET",
      headers: {
        accept: "application/json",
        ...headerOverrides,
      },
    }),
    locals: {
      outOfWorkspace,
    },
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
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
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

  it("exchanges workspace grant and redirects with auth cookies", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
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

    const exchangeLaunchSession = vi.fn(async () => ({
      ok: true,
      assertion: "grant_token",
    }));
    const event = createEvent(
      {
        exchange_token: "ex_123",
        state: "state_123",
        workspace_id: "ws_123",
        return_path: "/threads?view=mine",
      },
      {
        outOfWorkspace: mockHostedProvider({
          exchangeLaunchSession,
        }),
      },
    );

    await expect(POST(event)).rejects.toMatchObject({
      status: 303,
      location: "/o/local/w/acme/threads?view=mine",
    });

    expect(exchangeLaunchSession).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          workspaceId: "ws_123",
          exchangeToken: "ex_123",
          state: "state_123",
        }),
      }),
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe(
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

  it("accepts GET callback query params", async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce(
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

    const event = createGetEvent({
      exchange_token: "ex_123",
      state: "state_123",
      workspace_id: "ws_123",
      return_path: "/",
    });

    await expect(GET(event)).rejects.toMatchObject({
      status: 303,
      location: "/o/local/w/acme",
    });
  });

  it("returns provider session-exchange failures", async () => {
    const event = createEvent(
      {
        exchange_token: "ex_123",
        state: "state_123",
        workspace_id: "ws_123",
      },
      {
        outOfWorkspace: mockHostedProvider({
          exchangeLaunchSession: vi.fn(async () => ({
            ok: false,
            status: 409,
            code: "exchange_invalid",
            message: "exchange token has already been used",
          })),
        }),
      },
    );

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

  it("returns provider state_mismatch failures", async () => {
    const event = createEvent(
      {
        exchange_token: "ex_123",
        state: "state_123",
        workspace_id: "ws_123",
      },
      {
        outOfWorkspace: mockHostedProvider({
          exchangeLaunchSession: vi.fn(async () => ({
            ok: false,
            status: 401,
            code: "state_mismatch",
            message: "exchange token state is invalid",
          })),
        }),
      },
    );

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
    globalThis.fetch = vi.fn().mockResolvedValueOnce(
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

    const event = createEvent(
      {
        exchange_token: "ex_123",
        state: "state_123",
        workspace_id: "ws_123",
        return_path: "https://evil.test",
      },
      {
        outOfWorkspace: mockHostedProvider({
          exchangeLaunchSession: vi.fn(async () => ({
            ok: true,
            assertion: "grant_token",
          })),
        }),
      },
    );

    const response = await POST(event);
    expect(response.status).toBe(401);
    await expect(response.json()).resolves.toMatchObject({
      error: {
        code: "invalid_token",
      },
    });
  });
});
