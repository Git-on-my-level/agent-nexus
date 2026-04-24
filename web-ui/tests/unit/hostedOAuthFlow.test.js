// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("$app/environment", () => ({
  browser: true,
}));

import {
  buildHostedOAuthContinuation,
  clearHostedOAuthContinuation,
  deriveHostedOAuthRedirectURI,
  readHostedOAuthContinuation,
  resolveHostedPostAuthPath,
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
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });
  });

  it("stores, reads, and clears continuation keyed by oauth state", () => {
    storeHostedOAuthContinuation("oauth_state_1", {
      mode: "signup",
      next: "/hosted/dashboard",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });

    expect(readHostedOAuthContinuation("oauth_state_1")).toEqual({
      mode: "signup",
      next: "/hosted/dashboard",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });
    expect(readHostedOAuthContinuation("oauth_state_1")).toEqual({
      mode: "signup",
      next: "/hosted/dashboard",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_123",
    });
    clearHostedOAuthContinuation("oauth_state_1");
    expect(readHostedOAuthContinuation("oauth_state_1")).toBeNull();
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
});
