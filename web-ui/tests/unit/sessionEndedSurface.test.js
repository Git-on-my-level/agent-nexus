import { describe, expect, it, vi } from "vitest";

import { AuthErrorCode } from "../../src/lib/authErrorCodes.js";

const loadAgent = vi.hoisted(() => vi.fn());

vi.mock("$lib/server/authSession.js", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    loadWorkspaceAuthenticatedAgent: loadAgent,
    resolveWorkspaceSlugFromEvent: vi.fn(async () => ({
      organizationSlug: "local",
      workspaceSlug: "alpha",
      coreBaseUrl: "http://127.0.0.1:9000",
      error: null,
    })),
    clearWorkspaceAuthSession: vi.fn(),
    isRetryableWorkspaceAuthSessionError: vi.fn(() => false),
    isTerminalAccountSessionFailure: actual.isTerminalAccountSessionFailure,
  };
});

import { GET } from "../../src/routes/auth/session/+server.js";

describe("session ended surface (BFF)", () => {
  it("GET /auth/session returns 401 with session_ended_by_account_status for terminal account-status failures", async () => {
    const err = new Error("session ended");
    err.status = 401;
    err.details = {
      error: {
        code: AuthErrorCode.SESSION_ENDED_BY_ACCOUNT_STATUS,
        message: "Account inactive",
      },
    };
    loadAgent.mockRejectedValue(err);

    const res = await GET(
      /** @type {any} */ ({
        request: new Request("http://localhost/auth/session"),
        cookies: { get: vi.fn(), set: vi.fn(), delete: vi.fn() },
      }),
    );
    expect(res.status).toBe(401);
    const body = await res.json();
    expect(body.error?.code).toBe(
      AuthErrorCode.SESSION_ENDED_BY_ACCOUNT_STATUS,
    );
  });
});
