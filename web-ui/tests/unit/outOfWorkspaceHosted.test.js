import { describe, expect, it, vi } from "vitest";

import { createHostedProvider } from "../../src/lib/server/outOfWorkspace/hosted.js";
import { cpWorkspaceRows } from "../fixtures/workspaceAuth.js";

function createHosted(overrides = {}) {
  return createHostedProvider({
    controlPlaneBaseUrl: "http://127.0.0.1:8100",
    env: {
      ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
      ...overrides,
    },
  });
}

function eventWithFetch(fetchFn, cookieToken = "") {
  return {
    fetch: fetchFn,
    cookies: {
      get: vi.fn((name) =>
        name === "anx_cp_dev_access_token" ? cookieToken : "",
      ),
    },
    locals: {},
    request: new Request("http://localhost"),
    url: new URL("http://localhost"),
    params: {},
  };
}

describe("outOfWorkspace hosted provider", () => {
  it("resolves workspace by id from control-plane row", async () => {
    const provider = createHosted({
      ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "tok",
    });
    const fetchFn = vi.fn(
      async () =>
        new Response(JSON.stringify({ workspace: cpWorkspaceRows.minimal }), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    );
    const result = await provider.resolveWorkspaceById({
      event: /** @type {any} */ (eventWithFetch(fetchFn)),
      workspaceId: "ws-cp-1",
    });
    expect(result.kind).toBe("found");
    if (result.kind === "found") {
      expect(result.workspace.slug).toBe("alpha");
      expect(result.workspace.organizationSlug).toBe("acme");
    }
    expect(String(fetchFn.mock.calls[0][0])).toContain("/workspaces/ws-cp-1");
    expect(fetchFn.mock.calls[0][1].headers.get("authorization")).toBe(
      "Bearer tok",
    );
  });

  it("resolves by slug with cookie token when env token is unset", async () => {
    const provider = createHosted();
    const fetchFn = vi.fn(async (url) => {
      const u = String(url);
      if (u.includes("/organizations")) {
        return new Response(
          JSON.stringify({
            organizations: [{ id: "org-1", slug: "acme" }],
            next_cursor: "",
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        );
      }
      return new Response(
        JSON.stringify({
          workspaces: [cpWorkspaceRows.minimal],
          next_cursor: "",
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      );
    });
    const result = await provider.resolveWorkspaceBySlug({
      event: /** @type {any} */ (eventWithFetch(fetchFn, "cookie-tok")),
      organizationSlug: "acme",
      workspaceSlug: "alpha",
    });
    expect(result.kind).toBe("found");
    expect(fetchFn.mock.calls[0][1].headers.get("authorization")).toBe(
      "Bearer cookie-tok",
    );
  });

  it("memoizes slug lookup calls within one request event", async () => {
    const provider = createHosted();
    const fetchFn = vi.fn(async (url) => {
      const u = String(url);
      if (u.includes("/organizations")) {
        return new Response(
          JSON.stringify({
            organizations: [{ id: "org-1", slug: "acme" }],
            next_cursor: "",
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        );
      }
      return new Response(
        JSON.stringify({
          workspaces: [cpWorkspaceRows.minimal],
          next_cursor: "",
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      );
    });
    const event = /** @type {any} */ (eventWithFetch(fetchFn, "cookie-tok"));

    const first = await provider.resolveWorkspaceBySlug({
      event,
      organizationSlug: "acme",
      workspaceSlug: "alpha",
    });
    const second = await provider.resolveWorkspaceBySlug({
      event,
      organizationSlug: "acme",
      workspaceSlug: "alpha",
    });

    expect(first.kind).toBe("found");
    expect(second.kind).toBe("found");
    expect(fetchFn).toHaveBeenCalledTimes(2);
  });

  it("returns unauthenticated on slug lookup when no token is available", async () => {
    const provider = createHosted();
    const result = await provider.resolveWorkspaceBySlug({
      event: /** @type {any} */ (eventWithFetch(vi.fn())),
      organizationSlug: "acme",
      workspaceSlug: "alpha",
    });
    expect(result).toEqual({ kind: "unauthenticated" });
  });

  it("creates launch session redirect on happy path", async () => {
    const provider = createHosted({
      ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "tok",
    });
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            launch_session: {
              finish_url: "/workspaces/ws-cp-1/launch-finish?lid=abc",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        ),
    );
    const result = await provider.beginLaunchSession({
      event: /** @type {any} */ (eventWithFetch(fetchFn)),
      workspaceId: "ws-cp-1",
      returnPath: "/threads/1",
    });
    expect(result).toEqual({
      kind: "redirect",
      finishUrl: "/hosted/api/workspaces/ws-cp-1/launch-finish?lid=abc",
    });
  });

  it("returns needs_signin when launch session call is unauthorized", async () => {
    const provider = createHosted();
    const fetchFn = vi.fn(async () => new Response("", { status: 401 }));
    const result = await provider.beginLaunchSession({
      event: /** @type {any} */ (eventWithFetch(fetchFn)),
      workspaceId: "ws-cp-1",
      workspaceSlug: "alpha",
      returnPath: "/threads/1",
    });
    expect(result).toEqual({
      kind: "needs_signin",
      signInUrl:
        "/hosted/signin?workspace=alpha&workspace_id=ws-cp-1&return_path=%2Fthreads%2F1",
    });
  });

  it("canonicalizes session-exchange state mismatch", async () => {
    const provider = createHosted();
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "exchange_invalid",
              message: "exchange token state is invalid",
            },
          }),
          {
            status: 401,
            headers: { "content-type": "application/json" },
          },
        ),
    );
    const result = await provider.exchangeLaunchSession({
      event: /** @type {any} */ (eventWithFetch(fetchFn)),
      request: {
        workspaceId: "ws-cp-1",
        exchangeToken: "ex",
        state: "st",
      },
    });
    expect(result).toEqual({
      ok: false,
      status: 401,
      code: "state_mismatch",
      message: "exchange token state is invalid",
    });
  });

  it("returns assertion from successful session exchange", async () => {
    const provider = createHosted();
    const fetchFn = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            grant: { bearer_token: "grant-123" },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        ),
    );
    const result = await provider.exchangeLaunchSession({
      event: /** @type {any} */ (eventWithFetch(fetchFn)),
      request: {
        workspaceId: "ws-cp-1",
        exchangeToken: "ex",
        state: "st",
      },
    });
    expect(result).toEqual({ ok: true, assertion: "grant-123" });
  });

  it("does not forward env fallback token through hosted API proxy auth header", async () => {
    const provider = createHosted({
      ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "tok",
    });
    const fetchMock = vi.fn(
      async () =>
        new Response("{}", {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    try {
      await provider.proxyHostedApi({
        event: /** @type {any} */ ({
          request: new Request("http://localhost/hosted/api/workspaces"),
          url: new URL("http://localhost/hosted/api/workspaces"),
          cookies: {
            get: vi.fn(() => ""),
          },
        }),
        method: "GET",
        subpath: "workspaces",
      });
      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(
        fetchMock.mock.calls[0][1].headers.get("authorization"),
      ).toBeNull();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("uses env-only shell capability fields from provider env", () => {
    const provider = createHosted({
      PUBLIC_ANX_HOSTED_ACCOUNT_PATH: "/hosted/account",
      PUBLIC_ANX_CP_ORIGIN: "https://cp.public.example.test",
    });
    expect(provider.describeShellCapabilities()).toMatchObject({
      accountPath: "/hosted/account",
      publicOrigin: "https://cp.public.example.test",
    });
  });
});
