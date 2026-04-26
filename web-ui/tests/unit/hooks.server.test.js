import { beforeEach, describe, expect, it, vi } from "vitest";
import { CURRENT_VERSION } from "../../src/lib/generated/version.js";

const authSessionState = {
  currentSession: { accessToken: "expired-token" },
};

const envState = vi.hoisted(() => ({}));

const coreRouteCatalogMocks = vi.hoisted(() => ({
  isProxyableCommand: vi.fn(() => true),
}));

const proxyWorkspaceTargetMocks = vi.hoisted(() => ({
  resolveProxyTarget: vi.fn(() =>
    Promise.resolve({
      coreBaseUrl: "https://core.example.test",
      workspace: { slug: "ops" },
    }),
  ),
}));

const authSessionMocks = vi.hoisted(() => ({
  clearWorkspaceAuthSession: vi.fn(),
  getWorkspaceAuthSession: vi.fn(() => authSessionState.currentSession),
  isRetryableWorkspaceRefreshFailure: vi.fn(
    (error, options) =>
      error?.status === 401 &&
      (options?.hadAccessToken || options?.hadRefreshToken),
  ),
  readWorkspaceRefreshToken: vi.fn(() => "refresh-token"),
  refreshWorkspaceAuthSession: vi.fn(async () => {
    authSessionState.currentSession = { accessToken: "fresh-token" };
  }),
  shouldClearWorkspaceAuthSessionAfterRetryableFailure: vi.fn(() => false),
}));

vi.mock("$app/environment", () => ({
  dev: false,
}));

vi.mock("$env/dynamic/private", () => ({
  env: envState,
}));

vi.mock("$lib/coreRouteCatalog", () => ({
  isProxyableCommand: coreRouteCatalogMocks.isProxyableCommand,
}));

vi.mock("$lib/compat/workspaceCompat", () => ({
  getWorkspaceHeader: vi.fn(() => "ops"),
}));

vi.mock("$lib/workspacePaths", async (importOriginal) => {
  const mod = await importOriginal();
  return {
    ...mod,
    stripBasePath: vi.fn((pathname) => pathname),
  };
});

vi.mock("$lib/server/authSession", () => ({
  clearWorkspaceAuthSession: authSessionMocks.clearWorkspaceAuthSession,
  getWorkspaceAuthSession: authSessionMocks.getWorkspaceAuthSession,
  isRetryableWorkspaceRefreshFailure:
    authSessionMocks.isRetryableWorkspaceRefreshFailure,
  readWorkspaceRefreshToken: authSessionMocks.readWorkspaceRefreshToken,
  refreshWorkspaceAuthSession: authSessionMocks.refreshWorkspaceAuthSession,
  shouldClearWorkspaceAuthSessionAfterRetryableFailure:
    authSessionMocks.shouldClearWorkspaceAuthSessionAfterRetryableFailure,
}));

vi.mock("$lib/server/workspaceCatalog", () => ({
  loadWorkspaceCatalog: vi.fn(() => ({})),
}));

vi.mock("$lib/server/proxyWorkspaceTarget", () => ({
  resolveProxyTarget: proxyWorkspaceTargetMocks.resolveProxyTarget,
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
    proxyWorkspaceTargetMocks.resolveProxyTarget.mockImplementation(() =>
      Promise.resolve({
        coreBaseUrl: "https://core.example.test",
        workspace: { slug: "ops" },
      }),
    );
    coreRouteCatalogMocks.isProxyableCommand.mockReturnValue(true);
    authSessionMocks.isRetryableWorkspaceRefreshFailure.mockImplementation(
      (error, options) =>
        error?.status === 401 &&
        (options?.hadAccessToken || options?.hadRefreshToken),
    );
    authSessionMocks.shouldClearWorkspaceAuthSessionAfterRetryableFailure.mockReturnValue(
      false,
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
        url: new URL("https://anx.example.test/api/threads"),
        request: new Request("https://anx.example.test/api/threads", {
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
        url: new URL("https://anx.example.test/api/threads"),
        request: new Request("https://anx.example.test/api/threads", {
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
    authSessionMocks.isRetryableWorkspaceRefreshFailure.mockReturnValue(false);
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
        url: new URL("https://anx.example.test/api/threads"),
        request: new Request("https://anx.example.test/api/threads", {
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

  it("preserves the workspace session on retryable refresh failures when no access token was present", async () => {
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
        url: new URL("https://anx.example.test/api/threads"),
        request: new Request("https://anx.example.test/api/threads", {
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
      authSessionMocks.isRetryableWorkspaceRefreshFailure,
    ).toHaveBeenCalledWith(expect.anything(), {
      hadAccessToken: false,
      hadRefreshToken: true,
    });
    expect(
      authSessionMocks.shouldClearWorkspaceAuthSessionAfterRetryableFailure,
    ).toHaveBeenCalledWith(expect.anything(), "ops");
    expect(authSessionMocks.clearWorkspaceAuthSession).not.toHaveBeenCalled();
  });

  it("clears the workspace session after repeated retryable refresh failures", async () => {
    authSessionState.currentSession = { accessToken: "" };
    authSessionMocks.shouldClearWorkspaceAuthSessionAfterRetryableFailure.mockReturnValue(
      true,
    );
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
        url: new URL("https://anx.example.test/api/threads"),
        request: new Request("https://anx.example.test/api/threads", {
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

  it("returns 503 when proxyable but workspace has no coreBaseUrl", async () => {
    proxyWorkspaceTargetMocks.resolveProxyTarget.mockResolvedValueOnce({
      workspace: { slug: "ops" },
      coreBaseUrl: "",
    });

    const response = await handle({
      event: {
        url: new URL("https://anx.example.test/threads"),
        request: new Request("https://anx.example.test/threads", {
          method: "GET",
          headers: {
            accept: "application/json",
          },
        }),
      },
      resolve: vi.fn(),
    });

    expect(response.status).toBe(503);
    const payload = JSON.parse(await response.text());
    expect(payload.error.code).toBe("core_not_configured");
    expect(payload.error.message).toMatch(/coreBaseUrl/);
  });

  it("proxies GET /events/stream to core (SSE)", async () => {
    const sseBody = new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(": ok\n\n"));
        controller.close();
      },
    });
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(sseBody, {
        status: 200,
        headers: {
          "content-type": "text/event-stream",
        },
      }),
    );
    globalThis.fetch = fetchMock;

    const response = await handle({
      event: {
        url: new URL(
          "https://anx.example.test/events/stream?thread_id=thread-1",
        ),
        request: new Request(
          "https://anx.example.test/events/stream?thread_id=thread-1",
          {
            method: "GET",
            headers: {
              accept: "text/event-stream",
            },
          },
        ),
      },
      resolve: vi.fn(),
    });

    expect(response.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledWith(
      "https://core.example.test/events/stream?thread_id=thread-1",
      expect.objectContaining({
        method: "GET",
      }),
    );
  });

  it("adds configured CSP sources to document navigation responses", async () => {
    envState.ANX_UI_CSP_SCRIPT_SRC_EXTRA =
      "https://static.cloudflareinsights.com 'sha256-examplehash='";
    envState.ANX_UI_CSP_CONNECT_SRC_EXTRA = "https://cloudflareinsights.com";
    envState.ANX_UI_CSP_MANIFEST_SRC_EXTRA =
      "https://scalingforever.cloudflareaccess.com";

    const response = await handle({
      event: {
        url: new URL("https://anx.example.test/threads"),
        request: new Request("https://anx.example.test/threads", {
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
    expect(csp).toContain(
      "connect-src 'self' https://fonts.googleapis.com https://fonts.gstatic.com https://cloudflareinsights.com",
    );
    expect(csp).toContain(
      "manifest-src 'self' https://scalingforever.cloudflareaccess.com",
    );
    expect(response.headers.get("X-ANX-UI-Version")).toBe(CURRENT_VERSION);
  });

  it("bypasses proxy for nested workspace auth callback posts", async () => {
    const resolve = vi.fn(
      async () =>
        new Response("callback handled", {
          status: 200,
          headers: {
            "content-type": "text/plain",
          },
        }),
    );

    const response = await handle({
      event: {
        url: new URL("https://anx.example.test/o/local/w/ops/auth/callback"),
        request: new Request(
          "https://anx.example.test/o/local/w/ops/auth/callback",
          {
            method: "POST",
            headers: {
              accept: "text/html",
              "content-type": "application/x-www-form-urlencoded",
            },
            body: "exchange_token=tok&state=abc",
          },
        ),
      },
      resolve,
    });

    expect(resolve).toHaveBeenCalledTimes(1);
    expect(proxyWorkspaceTargetMocks.resolveProxyTarget).not.toHaveBeenCalled();
    expect(response.status).toBe(200);
    expect(await response.text()).toBe("callback handled");
  });

  it("bypasses proxy for callback posts with a trailing slash", async () => {
    const resolve = vi.fn(
      async () =>
        new Response("callback handled", {
          status: 200,
          headers: {
            "content-type": "text/plain",
          },
        }),
    );

    const response = await handle({
      event: {
        url: new URL("https://anx.example.test/o/local/w/ops/auth/callback/"),
        request: new Request(
          "https://anx.example.test/o/local/w/ops/auth/callback/",
          {
            method: "POST",
            headers: {
              accept: "text/html",
              "content-type": "application/x-www-form-urlencoded",
            },
            body: "exchange_token=tok&state=abc",
          },
        ),
      },
      resolve,
    });

    expect(resolve).toHaveBeenCalledTimes(1);
    expect(proxyWorkspaceTargetMocks.resolveProxyTarget).not.toHaveBeenCalled();
    expect(response.status).toBe(200);
    expect(await response.text()).toBe("callback handled");
  });
});
