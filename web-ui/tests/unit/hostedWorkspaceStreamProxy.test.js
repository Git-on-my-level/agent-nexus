import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthErrorCode } from "../../src/lib/authErrorCodes.js";
import { CURRENT_VERSION } from "../../src/lib/generated/version.js";

const envState = vi.hoisted(() => ({}));
const devState = vi.hoisted(() => ({ dev: true }));

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
}));

const authSessionMocks = vi.hoisted(() => ({
  getWorkspaceAuthSession: vi.fn(() => null),
  readWorkspaceRefreshToken: vi.fn(() => ""),
  refreshWorkspaceAuthSession: vi.fn(async () => null),
  clearWorkspaceAuthSession: vi.fn(),
  ensureWorkspaceAccessTokenForCoreProxy: vi.fn(async () => null),
  isRetryableWorkspaceRefreshFailure: vi.fn(() => false),
  shouldClearWorkspaceAuthSessionAfterRetryableFailure: vi.fn(() => false),
}));

vi.mock("$app/environment", () => devState);

vi.mock("$env/dynamic/private", () => ({
  env: envState,
}));

vi.mock("$lib/server/devLog", () => ({
  logServerEvent: vi.fn(),
}));

vi.mock("$lib/server/workspaceResolver.js", () => ({
  resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
}));

vi.mock("$lib/server/authSession", () => ({
  getWorkspaceAuthSession: authSessionMocks.getWorkspaceAuthSession,
  readWorkspaceRefreshToken: authSessionMocks.readWorkspaceRefreshToken,
  refreshWorkspaceAuthSession: authSessionMocks.refreshWorkspaceAuthSession,
  clearWorkspaceAuthSession: authSessionMocks.clearWorkspaceAuthSession,
  ensureWorkspaceAccessTokenForCoreProxy:
    authSessionMocks.ensureWorkspaceAccessTokenForCoreProxy,
  isRetryableWorkspaceRefreshFailure:
    authSessionMocks.isRetryableWorkspaceRefreshFailure,
  shouldClearWorkspaceAuthSessionAfterRetryableFailure:
    authSessionMocks.shouldClearWorkspaceAuthSessionAfterRetryableFailure,
}));

import {
  extractHostedWorkspaceUpstreamPath,
  isHostedWorkspaceStreamPath,
  proxyHostedWorkspaceStreamToCore,
  shouldProxyHostedWorkspaceStreamInDev,
} from "../../src/lib/server/hostedWorkspaceStreamProxy.js";

function createEvent(pathname, options = {}) {
  const { method = "GET", search = "", headers = {} } = options;
  const url = new URL(`http://localhost:5173${pathname}${search}`);
  return {
    url,
    request: new Request(url.toString(), {
      method,
      headers: new Headers(headers),
    }),
    cookies: { get: vi.fn(() => null) },
  };
}

async function readErrorJson(response) {
  return JSON.parse(await response.text());
}

describe("hostedWorkspaceStreamProxy helpers", () => {
  it("extracts upstream stream paths after the workspace prefix", () => {
    expect(
      extractHostedWorkspaceUpstreamPath("/ws/local-s-org/main/stream/events"),
    ).toBe("/stream/events");
    expect(
      extractHostedWorkspaceUpstreamPath(
        "/ws/local-s-org/main/stream/agent-notification-receipts",
      ),
    ).toBe("/stream/agent-notification-receipts");
    expect(
      extractHostedWorkspaceUpstreamPath("/ws/local-s-org/main/threads"),
    ).toBe("/threads");
  });

  it("detects stream paths only", () => {
    expect(isHostedWorkspaceStreamPath("/ws/acme/demo/stream/events")).toBe(
      true,
    );
    expect(isHostedWorkspaceStreamPath("/ws/acme/demo/stream")).toBe(true);
    expect(isHostedWorkspaceStreamPath("/ws/acme/demo/threads")).toBe(false);
  });

  it("enables dev bypass only when dev and control plane are configured", () => {
    devState.dev = true;
    envState.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    expect(shouldProxyHostedWorkspaceStreamInDev()).toBe(true);

    devState.dev = false;
    expect(shouldProxyHostedWorkspaceStreamInDev()).toBe(false);

    devState.dev = true;
    envState.ANX_HOSTED_DEV_STREAM_BYPASS = "0";
    expect(shouldProxyHostedWorkspaceStreamInDev()).toBe(false);
  });
});

describe("proxyHostedWorkspaceStreamToCore", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    devState.dev = true;
    envState.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    delete envState.ANX_HOSTED_DEV_STREAM_BYPASS;
    authSessionMocks.getWorkspaceAuthSession.mockReturnValue({
      accessToken: "workspace-token",
    });
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      coreBaseUrl: "http://localhost:5173/ws/acme/demo",
      workspace: { listenPort: 18100 },
    });
    globalThis.fetch = vi.fn(
      async () =>
        new Response("event: ping\n\ndata: {}\n\n", {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
    );
  });

  it("proxies stream requests to anx-core listen_port with query string", async () => {
    const pathname = "/ws/acme/demo/stream/events";
    const event = createEvent(pathname, {
      search: "?thread_id=thread-1&last_event_id=evt_1",
    });

    const response = await proxyHostedWorkspaceStreamToCore(event, pathname);

    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    expect(globalThis.fetch.mock.calls[0][0]).toBe(
      "http://127.0.0.1:18100/stream/events?thread_id=thread-1&last_event_id=evt_1",
    );
    const init = globalThis.fetch.mock.calls[0][1];
    expect(init.headers.get("authorization")).toBe("Bearer workspace-token");
    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toBe("text/event-stream");
    expect(response.headers.get("X-ANX-UI-Version")).toBe(CURRENT_VERSION);
  });

  it("returns workspace resolver errors unchanged", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: {
        status: 404,
        payload: {
          error: {
            code: "workspace_not_configured",
            message: "missing",
          },
        },
      },
    });
    const pathname = "/ws/acme/demo/stream/events";
    const event = createEvent(pathname);

    const response = await proxyHostedWorkspaceStreamToCore(event, pathname);

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(response.status).toBe(404);
    const body = await readErrorJson(response);
    expect(body.error.code).toBe("workspace_not_configured");
  });

  it("returns 503 when listen_port is missing", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      coreBaseUrl: "http://localhost:5173/ws/acme/demo",
      workspace: { listenPort: 0 },
    });
    const pathname = "/ws/acme/demo/stream/events";
    const event = createEvent(pathname);

    const response = await proxyHostedWorkspaceStreamToCore(event, pathname);

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(response.status).toBe(503);
    const body = await readErrorJson(response);
    expect(body.error.code).toBe("workspace_not_ready");
  });

  it("returns 400 for invalid workspace proxy paths", async () => {
    const pathname = "/ws/only-one";
    const event = createEvent(pathname);

    const response = await proxyHostedWorkspaceStreamToCore(event, pathname);

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(response.status).toBe(400);
    const body = await readErrorJson(response);
    expect(body.error.code).toBe(AuthErrorCode.INVALID_WORKSPACE_PROXY_PATH);
  });

  it("returns production-style 404 when dev bypass is disabled", async () => {
    devState.dev = false;
    const pathname = "/ws/acme/demo/stream/events";
    const event = createEvent(pathname);

    const response = await proxyHostedWorkspaceStreamToCore(event, pathname);

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(response.status).toBe(404);
    const body = await readErrorJson(response);
    expect(body.error.code).toBe("stream_bypasses_control_plane");
  });
});
