/**
 * Integration test: SvelteKit `+layout.server.js` for the workspace route,
 * wired to the REAL workspaceResolver + REAL out-of-workspace hosted fetch
 * layer, with only the network `fetch` stubbed.
 *
 * This is the closest-to-prod test we can run in vitest without spinning up
 * a real control plane. It catches the failure mode the user reported:
 * "I created a workspace and the login page shows 'Internal Error'", which
 * happens when the CP returns the workspace metadata but `core_origin` is
 * empty (the workspace is suspended or its anx-core process is not yet up).
 *
 * Without this test, regressions to the resolver chain or the layout's error
 * handling would re-introduce the opaque "Something went wrong" UI.
 */

import { beforeEach, describe, expect, it, vi } from "vitest";

const envState = vi.hoisted(() => ({}));

vi.mock("$env/dynamic/private", () => ({
  env: envState,
}));

import { load } from "../../src/routes/o/[organization]/w/[workspace]/+layout.server.js";

function resetEnv(overrides = {}) {
  for (const key of Object.keys(envState)) {
    delete envState[key];
  }
  Object.assign(envState, overrides);
}

function makeCpFetchMock({
  organizations = [{ id: "org_test", slug: "my-org" }],
  workspaces = [],
} = {}) {
  return vi.fn(async (url) => {
    const u = String(url);
    if (u.includes("/organizations") && !u.includes("/workspaces")) {
      return {
        ok: true,
        async json() {
          return { organizations, next_cursor: "" };
        },
      };
    }
    if (u.includes("/workspaces")) {
      return {
        ok: true,
        async json() {
          return { workspaces };
        },
      };
    }
    return {
      ok: false,
      status: 404,
      async json() {
        return { error: "unknown route in test" };
      },
    };
  });
}

function createEvent({
  organization = "my-org",
  workspace = "my-ws",
  pathname = "/o/my-org/w/my-ws/login",
  cookies = { oar_cp_dev_access_token: "tok-dev" },
  fetchFn,
} = {}) {
  return {
    params: { organization, workspace },
    url: new URL(`https://ui.example.test${pathname}`),
    fetch: fetchFn,
    cookies: {
      get: vi.fn((name) => cookies[name]),
      set: vi.fn(),
      delete: vi.fn(),
    },
  };
}

describe("workspace +layout.server.js (integration with real resolver)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Hosted mode (CP base URL set) so resolveWorkspaceInRoute will call
    // the (stubbed) CP fetch layer.
    resetEnv({
      ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
      ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "tok-dev",
    });
  });

  it("surfaces 503 workspace_not_ready (with structured code) when the CP returns a suspended workspace", async () => {
    const fetchFn = makeCpFetchMock({
      workspaces: [
        {
          id: "ws_test",
          organization_id: "org_test",
          organization_slug: "my-org",
          slug: "my-ws",
          display_name: "My WS",
          core_origin: "",
          public_origin: "",
          status: "suspended",
          desired_state: "ready",
        },
      ],
    });

    const event = createEvent({ fetchFn });

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown).toBeDefined();
    expect(thrown.status).toBe(503);
    // Critically, the structured code must propagate so +error.svelte can
    // render a useful message instead of the generic "Internal Error".
    expect(thrown.body?.code).toBe("workspace_not_ready");
    expect(thrown.body?.message).toMatch(/not ready/i);
    expect(thrown.body?.message).toMatch(/suspended/i);
    // We must NOT redirect to signin — the user is authenticated and the
    // workspace exists; bouncing to signin would loop them.
    expect(thrown.location).toBeUndefined();
    expect(event.cookies.set).not.toHaveBeenCalled();
  });

  it("surfaces 404 workspace_not_configured (with structured code) when the CP returns no matching workspace", async () => {
    const fetchFn = makeCpFetchMock({ workspaces: [] });

    const event = createEvent({ fetchFn });

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown).toBeDefined();
    expect(thrown.status).toBe(404);
    expect(thrown.body?.code).toBe("workspace_not_configured");
    // CP token cookie is set, so we should NOT redirect to signin.
    expect(thrown.location).toBeUndefined();
  });

  it("redirects 307 to /hosted/signin when CP returns no workspace AND there is no CP token at all", async () => {
    resetEnv({
      ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
      // No ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN, no cookie.
    });
    const fetchFn = makeCpFetchMock({ workspaces: [] });

    const event = createEvent({
      fetchFn,
      cookies: {}, // no oar_cp_dev_access_token
    });

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown).toBeDefined();
    expect(thrown.status).toBe(307);
    expect(thrown.location).toContain("/hosted/signin");
  });

  it("uses the nested login return_to as hosted return_path", async () => {
    resetEnv({
      ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
    });
    const fetchFn = makeCpFetchMock({ workspaces: [] });

    const event = createEvent({
      pathname: "/o/my-org/w/my-ws/login?return_to=%2Faccess",
      fetchFn,
      cookies: {},
    });

    let thrown;
    try {
      await load(event);
    } catch (err) {
      thrown = err;
    }

    expect(thrown).toBeDefined();
    expect(thrown.status).toBe(307);
    expect(thrown.location).toContain("/hosted/signin");
    expect(thrown.location).toContain("return_path=%2Faccess");
    expect(thrown.location).not.toContain("%2Flogin");
  });

  it("returns workspace data when the CP returns a fully ready workspace with a core origin", async () => {
    const fetchFn = makeCpFetchMock({
      workspaces: [
        {
          id: "ws_test",
          organization_id: "org_test",
          organization_slug: "my-org",
          slug: "my-ws",
          display_name: "My WS",
          core_origin: "http://127.0.0.1:18001",
          public_origin: "",
          status: "ready",
          desired_state: "ready",
        },
      ],
    });

    const event = createEvent({ fetchFn });
    event.cookies.get = vi.fn((name) =>
      name === "oar_ui_session_my-ws" ? "refresh-token" : "",
    );
    const result = await load(event);

    expect(result.workspace).toMatchObject({
      slug: "my-ws",
      organizationSlug: "my-org",
      coreBaseUrl: "http://127.0.0.1:18001",
    });
    expect(event.cookies.set).toHaveBeenCalledWith(
      expect.any(String),
      expect.stringContaining("my-org"),
      expect.objectContaining({ path: "/", httpOnly: true }),
    );
  });
});
