import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { createFormPostEvent } from "../helpers/svelteKitRequestEvent.js";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
}));

vi.mock("$lib/server/workspaceResolver", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
  };
});

vi.mock("$env/dynamic/private", () => ({
  env: {
    ANX_CONTROL_BASE_URL: "https://cp.example",
  },
}));

vi.mock("$app/paths", () => ({
  base: "",
}));

import { POST } from "../../src/routes/o/[organization]/w/[workspace]/auth/callback/+server.js";

const ORG_SLUG = "local";
const WORKSPACE_SLUG = "alpha";
const WORKSPACE_ID = "ws-callback-1";
const CORE_BASE = "http://127.0.0.1:9000";

function mockResolvedWorkspace() {
  workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
    organizationSlug: ORG_SLUG,
    workspaceSlug: WORKSPACE_SLUG,
    workspace: {
      slug: WORKSPACE_SLUG,
      coreBaseUrl: CORE_BASE,
      workspaceId: WORKSPACE_ID,
      id: WORKSPACE_ID,
    },
    error: null,
  });
}

async function readErrorJson(response) {
  const data = await response.json();
  return data;
}

describe("auth callback POST (+server)", () => {
  beforeEach(() => {
    mockResolvedWorkspace();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.clearAllMocks();
    workspaceResolverMocks.resolveWorkspaceInRoute.mockReset();
  });

  it("redirects on happy path, sets workspace cookies, uses sanitized return_path", async () => {
    const fetchMock = vi.fn(async (url) => {
      const u = String(url);
      if (u.includes("/session-exchange")) {
        return new Response(
          JSON.stringify({
            grant: { bearer_token: "assertion-happy" },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        );
      }
      if (u.includes("/auth/token")) {
        return new Response(
          JSON.stringify({
            tokens: {
              access_token: "at-happy",
              refresh_token: "rt-happy",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        );
      }
      throw new Error(`unexpected fetch URL: ${u}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const event = createFormPostEvent({
      exchange_token: "ex-tok",
      state: "state-val",
      workspace_id: WORKSPACE_ID,
      return_path: "/threads",
    });

    let thrown;
    try {
      await POST(/** @type {any} */ (event));
    } catch (e) {
      thrown = e;
    }

    expect(thrown).toMatchObject({
      status: 303,
      location: "/o/local/w/alpha/threads",
    });
    expect(
      event.cookieCalls.some((c) => c.name === "oar_ui_session_alpha"),
    ).toBe(true);
    expect(
      event.cookieCalls.find((c) => c.name === "oar_ui_session_alpha")?.value,
    ).toBe("rt-happy");
    expect(
      event.cookieCalls.some((c) => c.name === "oar_ui_access_alpha"),
    ).toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const tokenCall = fetchMock.mock.calls.find((c) =>
      String(c[0]).includes("/auth/token"),
    );
    expect(tokenCall).toBeDefined();
    expect(JSON.parse(String(tokenCall[1]?.body ?? "{}"))).toMatchObject({
      grant_type: "workspace_human_grant",
      assertion: "assertion-happy",
    });
  });

  it("returns state_mismatch when control plane session-exchange fails (400)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url) => {
        const u = String(url);
        if (u.includes("/session-exchange")) {
          return new Response(
            JSON.stringify({
              error: {
                code: "state_mismatch",
                message: "State does not match.",
              },
            }),
            {
              status: 400,
              headers: { "content-type": "application/json" },
            },
          );
        }
        throw new Error(`unexpected fetch: ${u}`);
      }),
    );

    const event = createFormPostEvent({
      exchange_token: "ex",
      state: "bad",
      workspace_id: WORKSPACE_ID,
    });
    const res = await POST(/** @type {any} */ (event));
    expect(res.status).toBe(400);
    const body = await readErrorJson(res);
    expect(body.error.code).toBe("state_mismatch");
    expect(body.error.message).toBeTruthy();
  });

  it("forwards exchange_expired from control plane (409)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url) => {
        const u = String(url);
        if (u.includes("/session-exchange")) {
          return new Response(
            JSON.stringify({
              error: {
                code: "exchange_expired",
                message: "Exchange token expired.",
              },
            }),
            {
              status: 409,
              headers: { "content-type": "application/json" },
            },
          );
        }
        throw new Error(`unexpected fetch: ${u}`);
      }),
    );

    const event = createFormPostEvent({
      exchange_token: "ex",
      state: "st",
      workspace_id: WORKSPACE_ID,
    });
    const res = await POST(/** @type {any} */ (event));
    expect(res.status).toBe(409);
    const body = await readErrorJson(res);
    expect(body.error.code).toBe("exchange_expired");
  });

  it("returns workspace_core_unreachable when oar-core /auth/token never connects", async () => {
    vi.useFakeTimers();

    vi.stubGlobal(
      "fetch",
      vi.fn(async (url) => {
        const u = String(url);
        if (u.includes("/session-exchange")) {
          return new Response(
            JSON.stringify({
              grant: { bearer_token: "assertion-net" },
            }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          );
        }
        if (u.includes("/auth/token")) {
          const err = new Error("fetch failed");
          err.cause = { code: "ECONNREFUSED", message: "connection refused" };
          throw err;
        }
        throw new Error(`unexpected fetch: ${u}`);
      }),
    );

    const event = createFormPostEvent({
      exchange_token: "ex",
      state: "st",
      workspace_id: WORKSPACE_ID,
    });

    const responsePromise = POST(/** @type {any} */ (event));
    await vi.runAllTimersAsync();
    const res = await responsePromise;

    expect(res.status).toBe(503);
    const body = await readErrorJson(res);
    expect(body.error.code).toBe("workspace_core_unreachable");
    expect(String(body.error.message)).toContain("127.0.0.1:9000");
  });

  it("forwards rate limiting from workspace core token exchange (429)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url) => {
        const u = String(url);
        if (u.includes("/session-exchange")) {
          return new Response(
            JSON.stringify({
              grant: { bearer_token: "assertion-rl" },
            }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          );
        }
        if (u.includes("/auth/token")) {
          return new Response(
            JSON.stringify({
              error: {
                code: "rate_limited",
                message: "Too many workspace_human_grant attempts.",
              },
            }),
            {
              status: 429,
              headers: { "content-type": "application/json" },
            },
          );
        }
        throw new Error(`unexpected fetch: ${u}`);
      }),
    );

    const event = createFormPostEvent({
      exchange_token: "ex",
      state: "st",
      workspace_id: WORKSPACE_ID,
    });
    const res = await POST(/** @type {any} */ (event));
    expect(res.status).toBe(429);
    const body = await readErrorJson(res);
    expect(body.error.code).toBe("rate_limited");
  });
});
