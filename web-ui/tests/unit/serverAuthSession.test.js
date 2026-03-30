import { afterEach, describe, expect, it, vi } from "vitest";

import {
  clearWorkspaceAccessToken,
  clearWorkspaceRefreshToken,
  getRecentRefreshResultCountForTests,
  handleWorkspaceAuthVerifyResponse,
  loadWorkspaceAuthenticatedAgent,
  refreshWorkspaceAuthSession,
  resetWorkspaceAuthRefreshStateForTests,
  writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken,
} from "../../src/lib/server/authSession.js";

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
  refreshToken = "",
  accessToken = "",
  workspaceSlug = "alpha",
} = {}) {
  const recorder = createCookieRecorder();
  if (refreshToken) {
    recorder.values.set(`oar_ui_session_${workspaceSlug}`, refreshToken);
  }
  if (accessToken) {
    recorder.values.set(`oar_ui_access_${workspaceSlug}`, accessToken);
  }
  return {
    recorder,
    event: {
      url: new URL("https://oar.example.com/auth/session"),
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
  it("writes HttpOnly and Secure refresh-token cookies on HTTPS", () => {
    const recorder = createCookieRecorder();
    const event = {
      url: new URL("https://oar.example.com/auth/session"),
      cookies: recorder.cookies,
    };

    writeWorkspaceRefreshToken(event, "alpha", "refresh-token");

    expect(recorder.setCalls).toHaveLength(1);
    expect(recorder.setCalls[0]).toMatchObject({
      name: "oar_ui_session_alpha",
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
      url: new URL("https://oar.example.com/auth/session"),
      cookies: recorder.cookies,
    };

    writeWorkspaceAccessToken(event, "alpha", "access-token");

    expect(recorder.setCalls).toHaveLength(1);
    expect(recorder.setCalls[0]).toMatchObject({
      name: "oar_ui_access_alpha",
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

    clearWorkspaceRefreshToken(event, "alpha");

    expect(recorder.deleteCalls).toEqual([
      {
        name: "oar_ui_session_alpha",
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

    clearWorkspaceAccessToken(event, "alpha");

    expect(recorder.deleteCalls).toEqual([
      {
        name: "oar_ui_access_alpha",
        options: {
          path: "/",
        },
      },
    ]);
  });

  it("sanitizes auth verify responses before returning them to the browser", async () => {
    const recorder = createCookieRecorder();
    const event = {
      url: new URL("https://oar.example.com/auth/passkey/login/verify"),
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
      workspaceSlug: "alpha",
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
        name: "oar_ui_session_alpha",
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
        name: "oar_ui_access_alpha",
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
      workspaceSlug: "alpha",
      coreBaseUrl: "https://core.example.com",
    });
    const secondRefresh = refreshWorkspaceAuthSession({
      event: second.event,
      workspaceSlug: "alpha",
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
    expect(first.recorder.values.get("oar_ui_session_alpha")).toBe(
      "next-refresh-token",
    );
    expect(second.recorder.values.get("oar_ui_session_alpha")).toBe(
      "next-refresh-token",
    );
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
        workspaceSlug: "alpha",
        coreBaseUrl: "https://core.example.com",
      }),
    ).resolves.toEqual({
      accessToken: "next-access-token",
      refreshToken: "next-refresh-token",
    });

    await expect(
      refreshWorkspaceAuthSession({
        event: second.event,
        workspaceSlug: "alpha",
        coreBaseUrl: "https://core.example.com",
      }),
    ).resolves.toEqual({
      accessToken: "next-access-token",
      refreshToken: "next-refresh-token",
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(second.recorder.values.get("oar_ui_session_alpha")).toBe(
      "next-refresh-token",
    );
    expect(second.recorder.values.get("oar_ui_access_alpha")).toBe(
      "next-access-token",
    );
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
      workspaceSlug: "alpha",
      coreBaseUrl: "https://core.example.com",
    });
    expect(getRecentRefreshResultCountForTests()).toBe(1);

    vi.advanceTimersByTime(60_001);

    await refreshWorkspaceAuthSession({
      event: second.event,
      workspaceSlug: "alpha",
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
        workspaceSlug: "alpha",
        coreBaseUrl: "https://core.example.com",
      }),
    ).rejects.toMatchObject({
      status: 503,
      code: "auth_session_retryable",
    });

    expect(recorder.values.get("oar_ui_session_alpha")).toBe("refresh-token");
    expect(recorder.values.get("oar_ui_access_alpha")).toBe(
      "expired-access-token",
    );
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
        workspaceSlug: "alpha",
        coreBaseUrl: "https://core.example.com",
      }),
    ).rejects.toMatchObject({
      status: 503,
      code: "auth_session_retryable",
    });

    expect(recorder.values.get("oar_ui_session_alpha")).toBe("refresh-token");
    expect(recorder.deleteCalls).toEqual([]);
  });

  it("treats refresh-only invalid_token failures as retryable until session rehydration retries settle", async () => {
    const { event, recorder } = createSessionEvent({
      refreshToken: "refresh-token",
    });
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
        workspaceSlug: "alpha",
        coreBaseUrl: "https://core.example.com",
      }),
    ).rejects.toMatchObject({
      status: 503,
      code: "auth_session_retryable",
    });

    expect(recorder.values.get("oar_ui_session_alpha")).toBe("refresh-token");
    expect(recorder.deleteCalls).toEqual([]);
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
        workspaceSlug: "alpha",
        coreBaseUrl: "https://core.example.com",
      }),
    ).resolves.toEqual({
      agent_id: "agent-1",
      actor_id: "actor-1",
      username: "passkey.user",
    });

    expect(recorder.values.get("oar_ui_session_alpha")).toBe(
      "next-refresh-token",
    );
    expect(recorder.values.get("oar_ui_access_alpha")).toBe(
      "next-access-token",
    );
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
});
