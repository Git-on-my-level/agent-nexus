import { beforeEach, describe, expect, it, vi } from "vitest";
import { CURRENT_VERSION } from "../../src/lib/generated/version.js";

const authSessionState = {
  currentSession: { accessToken: "expired-token" },
};

const envState = vi.hoisted(() => ({}));

const authSessionMocks = vi.hoisted(() => ({
  clearWorkspaceAuthSession: vi.fn(),
  getWorkspaceAuthSession: vi.fn(() => authSessionState.currentSession),
  isLikelyStaleWorkspaceRefreshFailure: vi.fn(
    (error, options) => error?.status === 401 && options?.hadAccessToken,
  ),
  readWorkspaceRefreshToken: vi.fn(() => "refresh-token"),
  refreshWorkspaceAuthSession: vi.fn(async () => {
    authSessionState.currentSession = { accessToken: "fresh-token" };
  }),
}));

vi.mock("$app/environment", () => ({
  dev: false,
}));

vi.mock("$env/dynamic/private", () => ({
  env: envState,
}));

vi.mock("$lib/coreRouteCatalog", () => ({
  isProxyableCommand: vi.fn(() => true),
}));

vi.mock("$lib/compat/workspaceCompat", () => ({
  getWorkspaceHeader: vi.fn(() => "ops"),
}));

vi.mock("$lib/workspacePaths", () => ({
  stripBasePath: vi.fn((pathname) => pathname),
}));

vi.mock("$lib/server/authSession", () => ({
  clearWorkspaceAuthSession: authSessionMocks.clearWorkspaceAuthSession,
  getWorkspaceAuthSession: authSessionMocks.getWorkspaceAuthSession,
  isLikelyStaleWorkspaceRefreshFailure:
    authSessionMocks.isLikelyStaleWorkspaceRefreshFailure,
  readWorkspaceRefreshToken: authSessionMocks.readWorkspaceRefreshToken,
  refreshWorkspaceAuthSession: authSessionMocks.refreshWorkspaceAuthSession,
}));

vi.mock("$lib/server/workspaceCatalog", () => ({
  loadWorkspaceCatalog: vi.fn(() => ({})),
}));

vi.mock("$lib/server/proxyWorkspaceTarget", () => ({
  resolveProxyTarget: vi.fn(() => ({
    coreBaseUrl: "https://core.example.test",
    workspace: { slug: "ops" },
  })),
}));

import { handle } from "../../src/hooks.server.js";

function bodyText(body) {
  if (!body) {
    return "";
  }
  if (body instanceof Uint8Array) {
    return new TextDecoder().decode(body);
  }
  if (body instanceof ArrayBuffer) {
    return new TextDecoder().decode(new Uint8Array(body));
  }
  return String(body);
}

describe("hooks proxy retry", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    authSessionState.currentSession = { accessToken: "expired-token" };
    authSessionMocks.isLikelyStaleWorkspaceRefreshFailure.mockImplementation(
      (error, options) => error?.status === 401 && options?.hadAccessToken,
    );
    for (const key of Object.keys(envState)) {
      delete envState[key];
    }
  });

  it("replays the original request body after refreshing workspace auth", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ error: { code: "invalid_token" } }), {
          status: 401,
          headers: {
            "content-type": "application/json",
          },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: {
            "content-type": "application/json",
          },
        }),
      );
    globalThis.fetch = fetchMock;

    const requestBody = JSON.stringify({ action: "update", value: 42 });
    const response = await handle({
      event: {
        url: new URL("https://oar.example.test/api/threads"),
        request: new Request("https://oar.example.test/api/threads", {
          method: "POST",
          headers: {
            accept: "application/json",
            "content-type": "application/json",
          },
          body: requestBody,
        }),
      },
      resolve: vi.fn(),
    });

    expect(response.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(2);

    const [firstUrl, firstInit] = fetchMock.mock.calls[0];
    const [secondUrl, secondInit] = fetchMock.mock.calls[1];
    expect(firstUrl).toBe("https://core.example.test/api/threads");
    expect(secondUrl).toBe("https://core.example.test/api/threads");
    expect(bodyText(firstInit.body)).toBe(requestBody);
    expect(bodyText(secondInit.body)).toBe(requestBody);
    expect(firstInit.headers.get("authorization")).toBe("Bearer expired-token");
    expect(secondInit.headers.get("authorization")).toBe("Bearer fresh-token");
    expect(authSessionMocks.refreshWorkspaceAuthSession).toHaveBeenCalledTimes(
      1,
    );
  });

  it("preserves the workspace session on stale rotated refresh failures", async () => {
    authSessionMocks.refreshWorkspaceAuthSession.mockRejectedValueOnce(
      Object.assign(new Error("stale rotated refresh token"), {
        status: 401,
        details: {
          error: {
            code: "invalid_token",
          },
        },
      }),
    );
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: "invalid_token" } }), {
        status: 401,
        headers: {
          "content-type": "application/json",
        },
      }),
    );
    globalThis.fetch = fetchMock;

    const response = await handle({
      event: {
        url: new URL("https://oar.example.test/api/threads"),
        request: new Request("https://oar.example.test/api/threads", {
          method: "GET",
          headers: {
            accept: "application/json",
          },
        }),
      },
      resolve: vi.fn(),
    });

    expect(response.status).toBe(401);
    expect(authSessionMocks.clearWorkspaceAuthSession).not.toHaveBeenCalled();
  });

  it("clears the workspace session on non-race refresh failures", async () => {
    authSessionMocks.isLikelyStaleWorkspaceRefreshFailure.mockReturnValue(
      false,
    );
    authSessionMocks.refreshWorkspaceAuthSession.mockRejectedValueOnce(
      Object.assign(new Error("agent revoked"), {
        status: 403,
        details: {
          error: {
            code: "agent_revoked",
          },
        },
      }),
    );
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: "invalid_token" } }), {
        status: 401,
        headers: {
          "content-type": "application/json",
        },
      }),
    );
    globalThis.fetch = fetchMock;

    const response = await handle({
      event: {
        url: new URL("https://oar.example.test/api/threads"),
        request: new Request("https://oar.example.test/api/threads", {
          method: "GET",
          headers: {
            accept: "application/json",
          },
        }),
      },
      resolve: vi.fn(),
    });

    expect(response.status).toBe(401);
    expect(authSessionMocks.clearWorkspaceAuthSession).toHaveBeenCalledWith(
      expect.anything(),
      "ops",
    );
  });

  it("clears the workspace session on invalid refresh failures when no access token was present", async () => {
    authSessionState.currentSession = { accessToken: "" };
    authSessionMocks.refreshWorkspaceAuthSession.mockRejectedValueOnce(
      Object.assign(new Error("invalid refresh token"), {
        status: 401,
        details: {
          error: {
            code: "invalid_token",
          },
        },
      }),
    );
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: "invalid_token" } }), {
        status: 401,
        headers: {
          "content-type": "application/json",
        },
      }),
    );
    globalThis.fetch = fetchMock;

    const response = await handle({
      event: {
        url: new URL("https://oar.example.test/api/threads"),
        request: new Request("https://oar.example.test/api/threads", {
          method: "GET",
          headers: {
            accept: "application/json",
          },
        }),
      },
      resolve: vi.fn(),
    });

    expect(response.status).toBe(401);
    expect(
      authSessionMocks.isLikelyStaleWorkspaceRefreshFailure,
    ).toHaveBeenCalledWith(expect.anything(), {
      hadAccessToken: false,
    });
    expect(authSessionMocks.clearWorkspaceAuthSession).toHaveBeenCalledWith(
      expect.anything(),
      "ops",
    );
  });

  it("adds configured CSP sources to document navigation responses", async () => {
    envState.OAR_UI_CSP_SCRIPT_SRC_EXTRA =
      "https://static.cloudflareinsights.com 'sha256-examplehash='";
    envState.OAR_UI_CSP_CONNECT_SRC_EXTRA = "https://cloudflareinsights.com";
    envState.OAR_UI_CSP_MANIFEST_SRC_EXTRA =
      "https://scalingforever.cloudflareaccess.com";

    const response = await handle({
      event: {
        url: new URL("https://oar.example.test/dtrinity"),
        request: new Request("https://oar.example.test/dtrinity", {
          method: "GET",
          headers: {
            accept: "text/html",
          },
        }),
      },
      resolve: vi.fn(
        () =>
          new Response("<!doctype html><html><body>ok</body></html>", {
            status: 200,
            headers: {
              "content-type": "text/html",
            },
          }),
      ),
    });

    const csp = response.headers.get("Content-Security-Policy");
    expect(csp).toContain(
      "script-src 'self' https://static.cloudflareinsights.com 'sha256-examplehash='",
    );
    expect(csp).toContain("connect-src 'self' https://cloudflareinsights.com");
    expect(csp).toContain(
      "manifest-src 'self' https://scalingforever.cloudflareaccess.com",
    );
    expect(response.headers.get("X-OAR-UI-Version")).toBe(CURRENT_VERSION);
  });
});
