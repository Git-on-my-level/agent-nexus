import { afterEach, describe, expect, it, vi } from "vitest";

import {
  clearWorkspaceAccessToken,
  clearWorkspaceRefreshToken,
  getAuthAccessCookieName,
  getAuthSessionCookieName,
  getRecentRefreshResultCountForTests,
  handleWorkspaceAuthVerifyResponse,
  loadWorkspaceAuthenticatedAgent,
  refreshWorkspaceAuthSession,
  resetWorkspaceAuthRefreshStateForTests,
  writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken,
} from "../../src/lib/server/authSession.js";

const DEFAULT_ORG = "local";
const DEFAULT_WS = "alpha";

function createCookieRecorder() {
  const setCalls = [];
  const deleteCalls = [];
  const values = new Map();
  return {
    setCalls,
    deleteCalls,
    values,
    cookies: {
      get(name) {
        return values.get(name) ?? null;
      },
      set(name, value, options) {
        values.set(name, value);
        setCalls.push({ name, value, options });
      },
      delete(name, options) {
        values.delete(name);
        deleteCalls.push({ name, options });
      },
    },
  };
}

function createSessionEvent({
  organizationSlug = DEFAULT_ORG,
  refreshToken = "",
  accessToken = "",
  workspaceSlug = DEFAULT_WS,
} = {}) {
  const recorder = createCookieRecorder();
  if (refreshToken) {
    recorder.values.set(
      getAuthSessionCookieName(organizationSlug, workspaceSlug),
      refreshToken,
    );
  }
  if (accessToken) {
    recorder.values.set(
      getAuthAccessCookieName(organizationSlug, workspaceSlug),
      accessToken,
    );
  }
  return {
    recorder,
    event: {
      url: new URL("https://anx.example.com/auth/session"),
      cookies: recorder.cookies,
    },
  };
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  resetWorkspaceAuthRefreshStateForTests();
});

describe("server auth session helpers", () => {
  it("uses distinct cookie names for the same workspace slug under different orgs", () => {
    expect(getAuthSessionCookieName("acme", "demo")).toBe(
      "anx_ui_session_acme__demo",
    );
    expect(getAuthSessionCookieName("other", "demo")).toBe(
      "anx_ui_session_other__demo",
    );
    expect(getAuthSessionCookieName("acme", "demo")).not.toBe(
      getAuthSessionCookieName("other", "demo"),
    );
    expect(getAuthAccessCookieName("acme", "demo")).not.toBe(
      getAuthAccessCookieName("other", "demo"),
    );
  });

  it("writes HttpOnly and Secure refresh-token cookies on HTTPS", () => {
    const recorder = createCookieRecorder();
    const event = {
      url: new URL("https://anx.example.com/auth/session"),
      cookies: recorder.cookies,
    };

    writeWorkspaceRefreshToken(event, DEFAULT_ORG, DEFAULT_WS, "refresh-token");

    expect(recorder.setCalls).toHaveLength(1);
    expect(recorder.setCalls[0]).toMatchObject({
      name: "anx_ui_session_local__alpha",
      value: "refresh-token",
      options: {
        httpOnly: true,
        maxAge: 30 * 24 * 60 * 60,
        sameSite: "lax",
        secure: true,
        path: "/",
      },
    });
  });

  it("writes HttpOnly and Secure access-token cookies on HTTPS", () => {
    const recorder = createCookieRecorder();
    const event = {
      url: new URL("https://anx.example.com/auth/session"),
      cookies: recorder.cookies,
    };

    writeWorkspaceAccessToken(event, DEFAULT_ORG, DEFAULT_WS, "access-token");

    expect(recorder.setCalls).toHaveLength(1);
    expect(recorder.setCalls[0]).toMatchObject({
      name: "anx_ui_access_local__alpha",
      value: "access-token",
      options: {
        httpOnly: true,
        maxAge: 15 * 60 + 60,
        sameSite: "lax",
        secure: true,
        path: "/",
      },
    });
  });

  it("clears refresh-token cookies with the same workspace scope", () => {
    const recorder = createCookieRecorder();
    const event = {
      cookies: recorder.cookies,
    };

    clearWorkspaceRefreshToken(event, DEFAULT_ORG, DEFAULT_WS);

    expect(recorder.deleteCalls).toEqual([
      {
        name: "anx_ui_session_local__alpha",
        options: {
          path: "/",
        },
      },
    ]);
  });

  it("clears access-token cookies with the same workspace scope", () => {
    const recorder = createCookieRecorder();
    const event = {
      cookies: recorder.cookies,
    };

    clearWorkspaceAccessToken(event, DEFAULT_ORG, DEFAULT_WS);

    expect(recorder.deleteCalls).toEqual([
      {
        name: "anx_ui_access_local__alpha",
        options: {
          path: "/",
        },
      },
    ]);
  });

  it("sanitizes auth verify responses before returning them to the browser", async () => {
    const recorder = createCookieRecorder();
    const event = {
      url: new URL("https://anx.example.com/auth/passkey/login/verify"),
      cookies: recorder.cookies,
    };
    const upstreamResponse = new Response(
      JSON.stringify({
        agent: {
          agent_id: "agent-1",
          actor_id: "actor-1",
          username: "passkey.user",
        },
        tokens: {
          access_token: "access-token",
          refresh_token: "refresh-token",
          token_type: "Bearer",
          expires_in: 3600,
        },
      }),
      {
        status: 200,
        headers: { "content-type": "application/json" },
      },
    );

    const response = await handleWorkspaceAuthVerifyResponse({
      event,
      organizationSlug: DEFAULT_ORG,
      workspaceSlug: DEFAULT_WS,
      upstreamResponse,
    });

    expect(await response.json()).toEqual({
      agent: {
        agent_id: "agent-1",
        actor_id: "actor-1",
        username: "passkey.user",
      },
    });
    expect(recorder.setCalls).toEqual([
      {
        name: "anx_ui_session_local__alpha",
        value: "refresh-token",
        options: {
          httpOnly: true,
          maxAge: 30 * 24 * 60 * 60,
          sameSite: "lax",
          secure: true,
          path: "/",
        },
      },
      {
        name: "anx_ui_access_local__alpha",
        value: "access-token",
        options: {
          httpOnly: true,
          maxAge: 15 * 60 + 60,
          sameSite: "lax",
          secure: true,
          path: "/",
        },
      },
    ]);
  });

  it("deduplicates concurrent refreshes that start with the same refresh token", async () => {
    const first = createSessionEvent({ refreshToken: "refresh-token" });
    const second = createSessionEvent({ refreshToken: "refresh-token" });
    let resolveRefresh;
    const fetchMock = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveRefresh = resolve;
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const firstRefresh = refreshWorkspaceAuthSession({
      event: first.event,
      organizationSlug: DEFAULT_ORG,
      workspaceSlug: DEFAULT_WS,
      coreBaseUrl: "https://core.example.com",
    });
    const secondRefresh = refreshWorkspaceAuthSession({
      event: second.event,
      organizationSlug: DEFAULT_ORG,
      workspaceSlug: DEFAULT_WS,
      coreBaseUrl: "https://core.example.com",
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);

    resolveRefresh(
      new Response(
        JSON.stringify({
          tokens: {
            access_token: "next-access-token",
            refresh_token: "next-refresh-token",
          },
        }),
        {
          status: 200,
          headers: { "content-type": "application/json" },
        },
      ),
    );

    await expect(firstRefresh).resolves.toEqual({
      accessToken: "next-access-token",
      refreshToken: "next-refresh-token",
    });
    await expect(secondRefresh).resolves.toEqual({
      accessToken: "next-access-token",
      refreshToken: "next-refresh-token",
    });
    const sessionName = getAuthSessionCookieName(DEFAULT_ORG, DEFAULT_WS);
    expect(first.recorder.values.get(sessionName)).toBe("next-refresh-token");
    expect(second.recorder.values.get(sessionName)).toBe("next-refresh-token");
  });

  it("runs separate in-flight refresh requests for the same token when organizations differ (same workspace slug)", async () => {
    const first = createSessionEvent({
      organizationSlug: "acme",
      refreshToken: "shared-rt",
      workspaceSlug: "demo",
    });
    const second = createSessionEvent({
      organizationSlug: "other",
      refreshToken: "shared-rt",
      workspaceSlug: "demo",
    });
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            tokens: {
              access_token: "at",
              refresh_token: "shared-rt",
            },
          }),
          { status: 200, headers: { "content-type": "application/json" } },
        ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await Promise.all([
      refreshWorkspaceAuthSession({
        event: first.event,
        organizationSlug: "acme",
        workspaceSlug: "demo",
        coreBaseUrl: "https://core.example.com",
      }),
      refreshWorkspaceAuthSession({
        event: second.event,
        organizationSlug: "other",
        workspaceSlug: "demo",
        coreBaseUrl: "https://core.example.com",
      }),
    ]);

    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("reuses a freshly rotated refresh result for a stale follow-up request", async () => {
    const first = createSessionEvent({ refreshToken: "refresh-token" });
    const second = createSessionEvent({ refreshToken: "refresh-token" });
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            tokens: {
              access_token: "next-access-token",
              refresh_token: "next-refresh-token",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      refreshWorkspaceAuthSession({
        event: first.event,
        organizationSlug: DEFAULT_ORG,
        workspaceSlug: DEFAULT_WS,
        coreBaseUrl: "https://core.example.com",
      }),
    ).resolves.toEqual({
      accessToken: "next-access-token",
      refreshToken: "next-refresh-token",
    });

    await expect(
      refreshWorkspaceAuthSession({
        event: second.event,
        organizationSlug: DEFAULT_ORG,
        workspaceSlug: DEFAULT_WS,
        coreBaseUrl: "https://core.example.com",
      }),
    ).resolves.toEqual({
      accessToken: "next-access-token",
      refreshToken: "next-refresh-token",
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(
      second.recorder.values.get(
        getAuthSessionCookieName(DEFAULT_ORG, DEFAULT_WS),
      ),
    ).toBe("next-refresh-token");
    expect(
      second.recorder.values.get(
        getAuthAccessCookieName(DEFAULT_ORG, DEFAULT_WS),
      ),
    ).toBe("next-access-token");
  });

  it("evicts expired replay entries when caching newer refresh results", async () => {
    vi.useFakeTimers();
    const first = createSessionEvent({ refreshToken: "refresh-token-1" });
    const second = createSessionEvent({ refreshToken: "refresh-token-2" });
    let accessTokenCounter = 0;
    const fetchMock = vi.fn(async () => {
      accessTokenCounter += 1;
      return new Response(
        JSON.stringify({
          tokens: {
            access_token: `next-access-token-${accessTokenCounter}`,
            refresh_token: `next-refresh-token-${accessTokenCounter}`,
          },
        }),
        {
          status: 200,
          headers: { "content-type": "application/json" },
        },
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    await refreshWorkspaceAuthSession({
      event: first.event,
      organizationSlug: DEFAULT_ORG,
      workspaceSlug: DEFAULT_WS,
      coreBaseUrl: "https://core.example.com",
    });
    expect(getRecentRefreshResultCountForTests()).toBe(1);

    vi.advanceTimersByTime(60_001);

    await refreshWorkspaceAuthSession({
      event: second.event,
      organizationSlug: DEFAULT_ORG,
      workspaceSlug: DEFAULT_WS,
      coreBaseUrl: "https://core.example.com",
    });

    expect(getRecentRefreshResultCountForTests()).toBe(1);
  });

  it("marks stale rotated refresh failures as retryable when the access token already expired", async () => {
    const { event, recorder } = createSessionEvent({
      refreshToken: "refresh-token",
      accessToken: "expired-access-token",
    });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            error: {
              code: "invalid_token",
              message: "expired access token",
            },
          }),
          {
            status: 401,
            headers: { "content-type": "application/json" },
          },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            error: {
              code: "invalid_token",
              message: "stale rotated refresh token",
            },
          }),
          {
            status: 401,
            headers: { "content-type": "application/json" },
          },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      loadWorkspaceAuthenticatedAgent({
        event,
        organizationSlug: DEFAULT_ORG,
        workspaceSlug: DEFAULT_WS,
        coreBaseUrl: "https://core.example.com",
      }),
    ).rejects.toMatchObject({
      status: 503,
      code: "auth_session_retryable",
    });

    expect(
      recorder.values.get(getAuthSessionCookieName(DEFAULT_ORG, DEFAULT_WS)),
    ).toBe("refresh-token");
    expect(
      recorder.values.get(getAuthAccessCookieName(DEFAULT_ORG, DEFAULT_WS)),
    ).toBe("expired-access-token");
    expect(recorder.values.get("anx_ui_auth_retry_local__alpha")).toBe("1");
    expect(recorder.deleteCalls).toEqual([]);
  });

  it("marks refresh-only invalid_token failures as retryable instead of clearing cookies", async () => {
    const { event, recorder } = createSessionEvent({
      refreshToken: "refresh-token",
    });
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: {
            code: "invalid_token",
            message: "stale rotated refresh token",
          },
        }),
        {
          status: 401,
          headers: { "content-type": "application/json" },
        },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      loadWorkspaceAuthenticatedAgent({
        event,
        organizationSlug: DEFAULT_ORG,
        workspaceSlug: DEFAULT_WS,
        coreBaseUrl: "https://core.example.com",
      }),
    ).rejects.toMatchObject({
      status: 503,
      code: "auth_session_retryable",
    });

    expect(
      recorder.values.get(getAuthSessionCookieName(DEFAULT_ORG, DEFAULT_WS)),
    ).toBe("refresh-token");
    expect(recorder.values.get("anx_ui_auth_retry_local__alpha")).toBe("1");
    expect(recorder.deleteCalls).toEqual([]);
  });

  it("clears the workspace auth session after repeated retryable refresh failures", async () => {
    const { event, recorder } = createSessionEvent({
      refreshToken: "refresh-token",
    });
    recorder.values.set("anx_ui_auth_retry_local__alpha", "1");
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: {
            code: "invalid_token",
            message: "token is invalid, expired, or revoked",
          },
        }),
        {
          status: 401,
          headers: { "content-type": "application/json" },
        },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      loadWorkspaceAuthenticatedAgent({
        event,
        organizationSlug: DEFAULT_ORG,
        workspaceSlug: DEFAULT_WS,
        coreBaseUrl: "https://core.example.com",
      }),
    ).resolves.toBeNull();

    expect(
      recorder.values.get(getAuthSessionCookieName(DEFAULT_ORG, DEFAULT_WS)),
    ).toBeUndefined();
    expect(
      recorder.values.get("anx_ui_auth_retry_local__alpha"),
    ).toBeUndefined();
    expect(recorder.deleteCalls).toEqual([
      {
        name: "anx_ui_auth_retry_local__alpha",
        options: { path: "/" },
      },
      {
        name: "anx_ui_session_local__alpha",
        options: { path: "/" },
      },
      {
        name: "anx_ui_access_local__alpha",
        options: { path: "/" },
      },
      {
        name: "anx_ui_auth_retry_local__alpha",
        options: { path: "/" },
      },
      {
        name: "anx_ui_session_alpha",
        options: { path: "/" },
      },
      {
        name: "anx_ui_access_alpha",
        options: { path: "/" },
      },
      {
        name: "anx_ui_auth_retry_alpha",
        options: { path: "/" },
      },
    ]);
  });

  it("recovers the authenticated agent from a refresh-only session after the access token expired", async () => {
    const { event, recorder } = createSessionEvent({
      refreshToken: "refresh-token",
    });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            tokens: {
              access_token: "next-access-token",
              refresh_token: "next-refresh-token",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            agent: {
              agent_id: "agent-1",
              actor_id: "actor-1",
              username: "passkey.user",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      loadWorkspaceAuthenticatedAgent({
        event,
        organizationSlug: DEFAULT_ORG,
        workspaceSlug: DEFAULT_WS,
        coreBaseUrl: "https://core.example.com",
      }),
    ).resolves.toEqual({
      agent_id: "agent-1",
      actor_id: "actor-1",
      username: "passkey.user",
    });

    expect(
      recorder.values.get(getAuthSessionCookieName(DEFAULT_ORG, DEFAULT_WS)),
    ).toBe("next-refresh-token");
    expect(
      recorder.values.get(getAuthAccessCookieName(DEFAULT_ORG, DEFAULT_WS)),
    ).toBe("next-access-token");
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "https://core.example.com/auth/token",
      expect.objectContaining({
        method: "POST",
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "https://core.example.com/agents/me",
      expect.objectContaining({
        method: "GET",
        headers: expect.objectContaining({
          authorization: "Bearer next-access-token",
        }),
      }),
    );
  });

  it("preserves hosted workspace proxy paths for session refresh and agent lookup", async () => {
    const { event } = createSessionEvent({
      organizationSlug: "scaling-forever",
      refreshToken: "refresh-token",
      workspaceSlug: "alpha",
    });
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            tokens: {
              access_token: "next-access-token",
              refresh_token: "next-refresh-token",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            agent: {
              agent_id: "agent-1",
              actor_id: "actor-1",
              username: "hosted.user",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      loadWorkspaceAuthenticatedAgent({
        event,
        organizationSlug: "scaling-forever",
        workspaceSlug: "alpha",
        coreBaseUrl: "http://localhost:5173/ws/scaling-forever/alpha",
      }),
    ).resolves.toEqual({
      agent_id: "agent-1",
      actor_id: "actor-1",
      username: "hosted.user",
    });

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "http://127.0.0.1:5173/ws/scaling-forever/alpha/auth/token",
      expect.objectContaining({
        method: "POST",
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "http://127.0.0.1:5173/ws/scaling-forever/alpha/agents/me",
      expect.objectContaining({
        method: "GET",
        headers: expect.objectContaining({
          authorization: "Bearer next-access-token",
        }),
      }),
    );
  });
});
