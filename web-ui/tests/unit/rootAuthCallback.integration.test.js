import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthErrorCode } from "../../src/lib/authErrorCodes.js";
import {
  mockHostedProvider,
  mockLocalProvider,
} from "../fixtures/workspaceAuth.js";

const runPost = vi.hoisted(() => vi.fn());
vi.mock("$lib/server/workspaceAuthCallbackPost.js", () => ({
  runWorkspaceAuthCallbackPost: runPost,
}));

vi.mock("$lib/server/devLog.js", () => ({
  logServerEvent: vi.fn(),
}));

import { POST } from "../../src/routes/auth/callback/+server.js";

function rootEvent(form, outOfWorkspace = mockLocalProvider()) {
  return {
    request: new Request("http://localhost/auth/callback", {
      method: "POST",
      body: form,
      headers: { accept: "application/json" },
    }),
    url: new URL("http://localhost/auth/callback"),
    params: {},
    locals: { outOfWorkspace },
    cookies: { get: vi.fn(() => "") },
    fetch: globalThis.fetch,
  };
}

describe("root /auth/callback POST", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns workspace_resolve_failed in local mode", async () => {
    const form = new FormData();
    form.set("workspace_id", "ws-1");
    const res = await POST(/** @type {any} */ (rootEvent(form)));
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error.code).toBe(AuthErrorCode.WORKSPACE_RESOLVE_FAILED);
    expect(body.error.reason).toBe("control_plane_unavailable");
    expect(runPost).not.toHaveBeenCalled();
  });

  it("returns workspace_resolve_failed when hosted provider is unauthenticated", async () => {
    const form = new FormData();
    form.set("workspace_id", "ws-1");
    const provider = mockHostedProvider({
      resolveWorkspaceById: vi.fn(async () => ({ kind: "unauthenticated" })),
    });
    const res = await POST(/** @type {any} */ (rootEvent(form, provider)));
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error.code).toBe(AuthErrorCode.WORKSPACE_RESOLVE_FAILED);
    expect(body.error.reason).toBe("control_plane_unauthenticated");
    expect(runPost).not.toHaveBeenCalled();
  });

  it("delegates to runWorkspaceAuthCallbackPost after provider lookup", async () => {
    const provider = mockHostedProvider({
      resolveWorkspaceById: vi.fn(async () => ({
        kind: "found",
        workspace: {
          organizationSlug: "acme",
          slug: "alpha",
        },
      })),
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
    await POST(/** @type {any} */ (rootEvent(form, provider)));

    expect(provider.resolveWorkspaceById).toHaveBeenCalled();
    expect(runPost).toHaveBeenCalled();
  });
});
