import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("$env/dynamic/private", () => ({
  env: { NODE_ENV: "development" },
}));

const catalogMocks = vi.hoisted(() => ({
  loadWorkspaceCatalog: vi.fn(() => ({ devActorMode: true })),
}));

vi.mock("$lib/server/workspaceCatalog.js", () => ({
  loadWorkspaceCatalog: catalogMocks.loadWorkspaceCatalog,
}));

const resolveMocks = vi.hoisted(() => ({
  resolveWorkspaceSlugFromEvent: vi.fn(),
}));

const authMocks = vi.hoisted(() => ({
  loadWorkspaceAuthenticatedAgent: vi.fn(),
  refreshWorkspaceAuthSession: vi.fn(),
  writeWorkspaceAccessToken: vi.fn(),
  writeWorkspaceRefreshToken: vi.fn(),
  isRetryableWorkspaceAuthSessionError: vi.fn(() => false),
}));

vi.mock("$lib/server/authSession.js", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceSlugFromEvent: resolveMocks.resolveWorkspaceSlugFromEvent,
    loadWorkspaceAuthenticatedAgent: authMocks.loadWorkspaceAuthenticatedAgent,
    refreshWorkspaceAuthSession: authMocks.refreshWorkspaceAuthSession,
    writeWorkspaceAccessToken: authMocks.writeWorkspaceAccessToken,
    writeWorkspaceRefreshToken: authMocks.writeWorkspaceRefreshToken,
    isRetryableWorkspaceAuthSessionError:
      authMocks.isRetryableWorkspaceAuthSessionError,
  };
});

vi.mock("$lib/server/devIdentityBundle.js", () => ({
  readLocalDevIdentityBundle: vi.fn(async () => ({
    personas: [
      {
        persona_id: "p1",
        actor_id: "actor-1",
        refresh_token: "rt-bundle",
        auth_username: "dev.user",
      },
    ],
  })),
}));

import { POST } from "../../src/routes/auth/dev/session/+server.js";

describe("POST /auth/dev/session", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resolveMocks.resolveWorkspaceSlugFromEvent.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "alpha",
      coreBaseUrl: "http://127.0.0.1:9000",
      error: null,
    });
  });

  it("returns dev_session_result reused when agent already matches persona", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue({
      agent_id: "a1",
      actor_id: "actor-1",
    });
    const event = {
      request: new Request("http://localhost/auth/dev/session", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ persona_id: "p1" }),
      }),
      cookies: { get: vi.fn(), set: vi.fn(), delete: vi.fn() },
    };
    const res = await POST(/** @type {any} */ (event));
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.dev_session_result).toBe("reused");
    expect(authMocks.refreshWorkspaceAuthSession).not.toHaveBeenCalled();
  });

  it("returns dev_session_result refreshed after bundle refresh succeeds", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({ agent_id: "a2", actor_id: "actor-1" });
    authMocks.refreshWorkspaceAuthSession.mockResolvedValue(undefined);
    const event = {
      request: new Request("http://localhost/auth/dev/session", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ persona_id: "p1" }),
      }),
      cookies: { get: vi.fn(), set: vi.fn(), delete: vi.fn() },
    };
    const res = await POST(/** @type {any} */ (event));
    const body = await res.json();
    expect(body.dev_session_result).toBe("refreshed");
    expect(authMocks.refreshWorkspaceAuthSession).toHaveBeenCalled();
  });
});
