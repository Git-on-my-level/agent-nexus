// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("$app/environment", () => ({
  browser: true,
}));

import {
  buildHostedOAuthRecoveryPath,
  buildHostedOAuthContinuation,
  clearHostedOAuthContinuation,
  createHostedLaunchSession,
  deriveHostedOAuthRedirectURI,
  friendlyHostedOAuthProviderError,
  hostedOAuthContinuationToken,
  normalizeHostedOAuthMode,
  readHostedOAuthError,
  readHostedOAuthContinuation,
  resolveHostedPostAuthPath,
  startHostedOAuthFlow,
  storeHostedOAuthContinuation,
} from "../../src/lib/hosted/oauthFlow.js";

describe("hosted oauth flow helpers", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("builds continuation from hosted auth URLs and preserves invite token", () => {
    const continuation = buildHostedOAuthContinuation(
      new URL(
        "http://localhost/hosted/signup?next=%2Fhosted%2Fdashboard&workspace=Acme&workspace_id=ws_123&return_path=%2Fthreads%2F1",
      ),
      {
        mode: "signup",
        inviteToken: "inv_123",
      },
    );
    expect(continuation).toEqual({
      mode: "signup",
      next: "/hosted/dashboard",
      organizationSlug: "",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });
  });

  it("normalizes hosted auth mode values", () => {
    expect(normalizeHostedOAuthMode("signup")).toBe("signup");
    expect(normalizeHostedOAuthMode(" signin ")).toBe("signin");
    expect(normalizeHostedOAuthMode("anything-else")).toBe("signin");
  });

  it("stores, reads, and clears continuation keyed by oauth state", () => {
    storeHostedOAuthContinuation("oauth_state_1", {
      mode: "signup",
      next: "/hosted/dashboard",
      organizationSlug: "david-zhang",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });

    expect(readHostedOAuthContinuation("oauth_state_1")).toEqual({
      mode: "signup",
      next: "/hosted/dashboard",
      organizationSlug: "david-zhang",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });
    expect(readHostedOAuthContinuation("oauth_state_1")).toEqual({
      mode: "signup",
      next: "/hosted/dashboard",
      organizationSlug: "david-zhang",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });
    clearHostedOAuthContinuation("oauth_state_1");
    expect(readHostedOAuthContinuation("oauth_state_1")).toBeNull();
  });

  it("serializes continuation into a canonical server-bound token", () => {
    expect(
      hostedOAuthContinuationToken({
        mode: "signup",
        next: "/hosted/dashboard",
        organizationSlug: "david-zhang",
        workspaceSlug: "acme",
        workspaceId: "ws_123",
        returnPath: "/threads/1",
        inviteToken: "inv_123",
      }),
    ).toBe(
      JSON.stringify({
        mode: "signup",
        next: "/hosted/dashboard",
        organizationSlug: "david-zhang",
        workspaceSlug: "acme",
        workspaceId: "ws_123",
        returnPath: "/threads/1",
        inviteToken: "inv_123",
      }),
    );
  });

  it("derives the canonical callback URL from browser location", () => {
    expect(
      deriveHostedOAuthRedirectURI("google", {
        origin: "https://app.example.com",
        pathname: "/hosted/oauth/google/callback",
      }),
    ).toBe("https://app.example.com/hosted/oauth/google/callback");
  });

  it("resolves signup vs signin post-auth destinations", () => {
    expect(resolveHostedPostAuthPath({ mode: "signup" })).toBe(
      "/hosted/onboarding/organization",
    );
    expect(resolveHostedPostAuthPath({ mode: "signin", next: "/work" })).toBe(
      "/work",
    );
    expect(resolveHostedPostAuthPath({ mode: "signin" })).toBe(
      "/hosted/dashboard",
    );
  });

  it("builds recovery auth paths that preserve continuation context", () => {
    expect(
      buildHostedOAuthRecoveryPath({
        mode: "signup",
        next: "/hosted/dashboard",
        organizationSlug: "david-zhang",
        workspaceSlug: "acme",
        workspaceId: "ws_123",
        returnPath: "/threads/1",
        inviteToken: "inv_123",
      }),
    ).toBe(
      "/hosted/signup?next=%2Fhosted%2Fdashboard&organization=david-zhang&workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F1&invite=inv_123",
    );
    expect(
      buildHostedOAuthRecoveryPath({
        mode: "signin",
        workspaceSlug: "acme",
        workspaceId: "ws_123",
        returnPath: "/threads/1",
      }),
    ).toBe(
      "/hosted/signin?workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F1",
    );
  });

  it("reads hosted oauth errors from API responses", async () => {
    await expect(
      readHostedOAuthError({
        json: async () => ({ error: { message: "oauth_invalid_state" } }),
        statusText: "Unauthorized",
      }),
    ).resolves.toBe("oauth_invalid_state");

    await expect(
      readHostedOAuthError({
        json: async () => {
          throw new Error("bad json");
        },
        statusText: "Unauthorized",
      }),
    ).resolves.toBe("Unauthorized");
  });

  it("starts oauth flows and stores continuation state once", async () => {
    const cpFetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({
        oauth_session: {
          authorization_url:
            "https://github.com/login/oauth/authorize?state=state_2",
          state: "state_2",
        },
      }),
    }));

    await expect(
      startHostedOAuthFlow({
        cpFetch,
        provider: "github",
        pageUrl: new URL(
          "http://localhost/hosted/signup?workspace=Acme&workspace_id=ws_123&return_path=%2Fthreads%2F1",
        ),
        mode: "signup",
        inviteToken: "inv_123",
      }),
    ).resolves.toEqual({
      authorizationURL:
        "https://github.com/login/oauth/authorize?state=state_2",
      state: "state_2",
      provider: "github",
    });

    expect(cpFetch).toHaveBeenCalledWith("account/oauth/github/start", {
      method: "POST",
      body: JSON.stringify({
        continuation_token: JSON.stringify({
          mode: "signup",
          next: "",
          organizationSlug: "",
          workspaceSlug: "acme",
          workspaceId: "ws_123",
          returnPath: "/threads/1",
          inviteToken: "inv_123",
        }),
      }),
    });
    expect(readHostedOAuthContinuation("state_2")).toEqual({
      mode: "signup",
      next: "",
      organizationSlug: "",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });
  });

  it("creates launch sessions with sanitized return paths", async () => {
    const cpFetch = vi.fn(async () => ({
      ok: true,
      json: async () => ({
        launch_session: {
          finish_url: "https://hosted.example.com/auth/callback",
        },
      }),
    }));

    await expect(
      createHostedLaunchSession({
        cpFetch,
        workspaceId: "ws_123",
        returnPath: "https://evil.example.com/nope",
      }),
    ).resolves.toEqual({
      launch_session: {
        finish_url: "https://hosted.example.com/auth/callback",
      },
    });

    expect(cpFetch).toHaveBeenCalledWith("workspaces/ws_123/launch-sessions", {
      method: "POST",
      body: JSON.stringify({
        return_path: "/",
      }),
    });
  });

  it("resolves slug-only launch continuations after sign-in", async () => {
    const cpFetch = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          organizations: [
            { id: "org_other", slug: "other" },
            { id: "org_123", slug: "david-zhang" },
          ],
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          workspaces: [{ id: "ws_123", slug: "personal" }],
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          launch_session: {
            finish_url: "/workspaces/ws_123/launch-finish",
          },
        }),
      });

    await expect(
      createHostedLaunchSession({
        cpFetch,
        organizationSlug: "david-zhang",
        workspaceSlug: "personal",
        returnPath: "/boards/board_1?card=card_1",
      }),
    ).resolves.toEqual({
      launch_session: {
        finish_url: "/workspaces/ws_123/launch-finish",
      },
    });

    expect(cpFetch).toHaveBeenNthCalledWith(1, "organizations?limit=100");
    expect(cpFetch).toHaveBeenNthCalledWith(
      2,
      "workspaces?organization_id=org_123&limit=200",
    );
    expect(cpFetch).toHaveBeenNthCalledWith(
      3,
      "workspaces/ws_123/launch-sessions",
      {
        method: "POST",
        body: JSON.stringify({
          return_path: "/boards/board_1?card=card_1",
        }),
      },
    );
  });

  it("formats provider cancellation copy from shared helper", () => {
    expect(friendlyHostedOAuthProviderError("access_denied", "signin")).toBe(
      "Sign-in was canceled at the identity provider.",
    );
    expect(
      friendlyHostedOAuthProviderError("cancelled_on_user_request", "signup"),
    ).toBe("Signup was canceled at the identity provider.");
  });
});
