import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthErrorCode } from "../../src/lib/authErrorCodes.js";
import { CURRENT_VERSION } from "../../src/lib/generated/version.js";

const envState = vi.hoisted(() => ({}));

const authSessionMocks = vi.hoisted(() => ({
  getWorkspaceAuthSession: vi.fn(() => null),
}));

const logServerEventMock = vi.hoisted(() => vi.fn());

vi.mock("$lib/server/devLog", () => ({
  logServerEvent: logServerEventMock,
}));

vi.mock("$env/dynamic/private", () => ({
  env: envState,
}));

vi.mock("$lib/server/authSession", () => ({
  getWorkspaceAuthSession: authSessionMocks.getWorkspaceAuthSession,
}));

import {
  classifyWorkspaceProxyPathShape,
  proxyToControlPlaneWorkspace,
} from "../../src/lib/server/hostedWorkspaceProxy.js";

function createEvent(pathname, options = {}) {
  const { method = "GET", body, search = "", headers = {} } = options;
  const url = new URL(`http://localhost:5173${pathname}${search}`);
  const h = new Headers(headers);
  return {
    url,
    request: new Request(url.toString(), { method, body, headers: h }),
    cookies: { get: vi.fn(() => null) },
  };
}

async function readErrorJson(response) {
  return JSON.parse(await response.text());
}

describe("classifyWorkspaceProxyPathShape", () => {
  it("classifies /ws/ as prefix_only", () => {
    expect(classifyWorkspaceProxyPathShape("/ws/")).toBe("prefix_only");
  });

  it("classifies /ws/demo as too_few_segments", () => {
    expect(classifyWorkspaceProxyPathShape("/ws/demo")).toBe("too_few_segments");
  });

  it("classifies /ws//demo/livez as empty_segment", () => {
    expect(classifyWorkspaceProxyPathShape("/ws//demo/livez")).toBe(
      "empty_segment",
    );
  });

  it("classifies non-/ws/ as wrong_prefix", () => {
    expect(classifyWorkspaceProxyPathShape("/api/ws/x")).toBe("wrong_prefix");
  });

  it("classifies would-be-valid /ws/a/b/... as unknown_invalid", () => {
    expect(
      classifyWorkspaceProxyPathShape("/ws/scaling-forever/personal/api"),
    ).toBe("unknown_invalid");
  });
});

describe("hostedWorkspaceProxy (proxyToControlPlaneWorkspace)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    logServerEventMock.mockClear();
    authSessionMocks.getWorkspaceAuthSession.mockReturnValue(null);
    for (const key of Object.keys(envState)) {
      delete envState[key];
    }
    globalThis.fetch = vi.fn();
  });

  it("returns 503 JSON with X-ANX-UI-Version when ANX_CONTROL_BASE_URL is missing, no fetch", async () => {
    const event = createEvent("/ws/org/ws");

    const response = await proxyToControlPlaneWorkspace(event, "/ws/org/ws");

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(response.status).toBe(503);
    expect(response.headers.get("X-ANX-UI-Version")).toBe(CURRENT_VERSION);
    expect(response.headers.get("content-type")).toBe("application/json");
    const body = await readErrorJson(response);
    expect(body.error.code).toBe("control_plane_not_configured");
  });

  it("returns 400 for invalid /ws/... with INVALID_WORKSPACE_PROXY_PATH, no fetch, one safe log", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    const event = createEvent("/ws/only-one-segment", { method: "PUT" });

    const response = await proxyToControlPlaneWorkspace(
      event,
      "/ws/only-one-segment",
    );

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(response.status).toBe(400);
    expect(response.headers.get("X-ANX-UI-Version")).toBe(CURRENT_VERSION);
    expect(response.headers.get("content-type")).toBe("application/json");
    const body = await readErrorJson(response);
    expect(body.error.code).toBe(AuthErrorCode.INVALID_WORKSPACE_PROXY_PATH);
    expect(logServerEventMock).toHaveBeenCalledTimes(1);
    expect(logServerEventMock).toHaveBeenCalledWith(
      "workspace.proxy.invalid_path",
      {
        code: AuthErrorCode.INVALID_WORKSPACE_PROXY_PATH,
        path_shape: "too_few_segments",
        method: "PUT",
      },
    );
  });

  it("returns 400 for /ws with no org/workspace segments (same error contract)", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    const event = createEvent("/ws");

    const response = await proxyToControlPlaneWorkspace(event, "/ws");

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(response.status).toBe(400);
    const body = await readErrorJson(response);
    expect(body.error.code).toBe(AuthErrorCode.INVALID_WORKSPACE_PROXY_PATH);
  });

  it("forwards a valid /ws/{org}/{ws}/... to the control plane with search string", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    globalThis.fetch = vi.fn(async () => new Response("ok", { status: 200 }));

    const pathname = "/ws/scaling-forever/personal/api/x";
    const event = createEvent(pathname, { search: "?q=1&x=y" });

    const response = await proxyToControlPlaneWorkspace(event, pathname);

    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    expect(globalThis.fetch.mock.calls[0][0]).toBe(
      "http://control.example.test/ws/scaling-forever/personal/api/x?q=1&x=y",
    );
    expect(response.status).toBe(200);
    expect(response.headers.get("X-ANX-UI-Version")).toBe(CURRENT_VERSION);
  });

  it("forwards POST with body to upstream", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    const payload = new Uint8Array([1, 2, 3]);
    globalThis.fetch = vi.fn(async () => new Response("created", { status: 201 }));
    const pathname = "/ws/o/w/thread";
    const event = createEvent(pathname, {
      method: "POST",
      body: payload,
    });

    const response = await proxyToControlPlaneWorkspace(event, pathname);

    expect(response.status).toBe(201);
    const init = globalThis.fetch.mock.calls[0][1];
    expect(init?.method).toBe("POST");
    expect(init?.body).toBeDefined();
  });

  it("prefers UI session Authorization over incoming header", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    authSessionMocks.getWorkspaceAuthSession.mockReturnValue({
      accessToken: "session-token",
    });
    globalThis.fetch = vi.fn(async () => new Response("ok", { status: 200 }));
    const pathname = "/ws/acme/ops/hi";
    const event = createEvent(pathname, {
      headers: { authorization: "Bearer incoming" },
    });

    await proxyToControlPlaneWorkspace(event, pathname);

    const init = globalThis.fetch.mock.calls[0][1];
    expect(init.headers.get("authorization")).toBe("Bearer session-token");
  });

  it("forwards incoming Authorization when no session access token", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    globalThis.fetch = vi.fn(async () => new Response("ok", { status: 200 }));
    const pathname = "/ws/acme/ops/hi";
    const event = createEvent(pathname, {
      headers: { authorization: "Bearer incoming" },
    });

    await proxyToControlPlaneWorkspace(event, pathname);

    const init = globalThis.fetch.mock.calls[0][1];
    expect(init.headers.get("authorization")).toBe("Bearer incoming");
  });

  it("strips content-encoding and content-length and sets X-ANX-UI-Version on upstream", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    globalThis.fetch = vi.fn(
      async () =>
        new Response("{}", {
          status: 200,
          headers: {
            "content-type": "application/json",
            "content-encoding": "gzip",
            "content-length": "999",
            "x-up": "cp",
          },
        }),
    );
    const pathname = "/ws/a/b/x";
    const event = createEvent(pathname);
    const response = await proxyToControlPlaneWorkspace(event, pathname);
    expect(response.headers.get("content-encoding")).toBeNull();
    expect(response.headers.get("content-length")).toBeNull();
    expect(response.headers.get("X-ANX-UI-Version")).toBe(CURRENT_VERSION);
    expect(response.headers.get("x-up")).toBe("cp");
  });

  it("returns 503 control_plane_unreachable on fetch failure", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    globalThis.fetch = vi.fn(async () => {
      throw new Error("ECONNREFUSED");
    });
    const pathname = "/ws/a/b/x";
    const event = createEvent(pathname);
    const response = await proxyToControlPlaneWorkspace(event, pathname);
    expect(response.status).toBe(503);
    const body = await readErrorJson(response);
    expect(body.error.code).toBe("control_plane_unreachable");
    expect(response.headers.get("X-ANX-UI-Version")).toBe(CURRENT_VERSION);
  });

  it("resolves the workspace session by organization and workspace to avoid same-slug collisions", async () => {
    envState.ANX_CONTROL_BASE_URL = "http://control.example.test";
    authSessionMocks.getWorkspaceAuthSession.mockImplementation(
      (_event, org, workspace) => ({
        accessToken: `tok-${org}-${workspace}`,
      }),
    );
    globalThis.fetch = vi.fn(async () => new Response("ok", { status: 200 }));
    const pathA = "/ws/acme/personal/threads";
    const pathB = "/ws/other/personal/threads";
    const eventA = createEvent(pathA);
    const eventB = createEvent(pathB);

    await proxyToControlPlaneWorkspace(eventA, pathA);
    await proxyToControlPlaneWorkspace(eventB, pathB);

    expect(authSessionMocks.getWorkspaceAuthSession).toHaveBeenNthCalledWith(
      1,
      eventA,
      "acme",
      "personal",
    );
    expect(authSessionMocks.getWorkspaceAuthSession).toHaveBeenNthCalledWith(
      2,
      eventB,
      "other",
      "personal",
    );
    const [, initA] = globalThis.fetch.mock.calls[0];
    const [, initB] = globalThis.fetch.mock.calls[1];
    expect(initA.headers.get("authorization")).toBe("Bearer tok-acme-personal");
    expect(initB.headers.get("authorization")).toBe("Bearer tok-other-personal");
  });
});
