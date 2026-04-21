import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthErrorCode } from "../../src/lib/authErrorCodes.js";

const privateEnv = vi.hoisted(() => ({
  ANX_CONTROL_BASE_URL: "",
  ANX_SAAS_PACKED_HOST_DEV: "",
  ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "",
}));

vi.mock("$env/dynamic/private", () => ({
  env: privateEnv,
}));

const runPost = vi.hoisted(() => vi.fn());
vi.mock("$lib/server/workspaceAuthCallbackPost.js", () => ({
  runWorkspaceAuthCallbackPost: runPost,
}));

const fetchById = vi.hoisted(() => vi.fn());
vi.mock("$lib/server/controlPlaneWorkspace.js", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    fetchWorkspaceByIdFromControlPlane: fetchById,
  };
});

vi.mock("$lib/server/devLog.js", () => ({
  logServerEvent: vi.fn(),
}));

import { POST } from "../../src/routes/auth/callback/+server.js";

function rootEvent(form) {
  return {
    request: new Request("http://localhost/auth/callback", {
      method: "POST",
      body: form,
      headers: { accept: "application/json" },
    }),
    url: new URL("http://localhost/auth/callback"),
    params: {},
    cookies: { get: vi.fn(() => "") },
    fetch: globalThis.fetch,
  };
}

describe("root /auth/callback POST", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    privateEnv.ANX_CONTROL_BASE_URL = "";
    privateEnv.ANX_SAAS_PACKED_HOST_DEV = "";
    privateEnv.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN = "";
  });

  it("returns workspace_resolve_failed in local mode (no CP URL)", async () => {
    const form = new FormData();
    form.set("workspace_id", "ws-1");
    const res = await POST(/** @type {any} */ (rootEvent(form)));
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error.code).toBe(AuthErrorCode.WORKSPACE_RESOLVE_FAILED);
    expect(runPost).not.toHaveBeenCalled();
  });

  it("returns workspace_resolve_failed in hosted mode when lookup by id is disabled", async () => {
    privateEnv.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    privateEnv.ANX_SAAS_PACKED_HOST_DEV = "";
    const form = new FormData();
    form.set("workspace_id", "ws-1");
    const res = await POST(/** @type {any} */ (rootEvent(form)));
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error.code).toBe(AuthErrorCode.WORKSPACE_RESOLVE_FAILED);
    expect(body.error.reason).toBe("hosted_mode_workspace_id_lookup_disabled");
    expect(fetchById).not.toHaveBeenCalled();
    expect(runPost).not.toHaveBeenCalled();
  });

  it("delegates to runWorkspaceAuthCallbackPost after CP lookup in packed-host dev", async () => {
    privateEnv.ANX_CONTROL_BASE_URL = "http://127.0.0.1:8100";
    privateEnv.ANX_SAAS_PACKED_HOST_DEV = "1";
    privateEnv.ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN = "tok";
    fetchById.mockResolvedValue({
      organizationSlug: "acme",
      slug: "alpha",
      coreBaseUrl: "http://127.0.0.1:9000",
      id: "ws-1",
    });
    runPost.mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );

    const form = new FormData();
    form.set("workspace_id", "ws-1");
    form.set("exchange_token", "x");
    form.set("state", "s");
    await POST(/** @type {any} */ (rootEvent(form)));

    expect(fetchById).toHaveBeenCalled();
    expect(runPost).toHaveBeenCalled();
  });
});
