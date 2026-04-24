// @vitest-environment jsdom
import { cleanup, render, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

let currentPage = {
  url: new URL(
    "/hosted/oauth/google/callback?code=abc&state=state_1",
    window.location.origin,
  ),
  params: { provider: "google" },
};

const mockGoto = vi.fn();

vi.mock("$app/environment", () => ({
  browser: true,
}));

vi.mock("$app/navigation", () => ({
  goto: (...args) => mockGoto(...args),
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: (fn) => {
      fn(currentPage);
      return () => {};
    },
  },
}));

vi.mock("$lib/hosted/cpFetch.js", () => ({
  hostedCpFetch: vi.fn(),
  persistHostedCpAccessToken: vi.fn(),
}));

vi.mock("$lib/hosted/session.js", () => ({
  loadHostedSession: vi.fn(async () => ({})),
}));

import HostedOAuthCallbackPage from "../../src/routes/hosted/oauth/[provider]/callback/+page.svelte";
import {
  hostedCpFetch,
  persistHostedCpAccessToken,
} from "$lib/hosted/cpFetch.js";
import { loadHostedSession } from "$lib/hosted/session.js";
import {
  readHostedOAuthContinuation,
  storeHostedOAuthContinuation,
} from "../../src/lib/hosted/oauthFlow.js";

describe("hosted oauth callback page", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    currentPage = {
      url: new URL(
        "/hosted/oauth/google/callback?code=abc&state=state_1",
        window.location.origin,
      ),
      params: { provider: "google" },
    };
    window.history.replaceState(
      {},
      "",
      `${currentPage.url.pathname}${currentPage.url.search}`,
    );
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("finishes signup oauth, persists the session, and continues to onboarding", async () => {
    storeHostedOAuthContinuation("state_1", {
      mode: "signup",
      next: "",
      workspaceSlug: "",
      workspaceId: "",
      returnPath: "/",
      inviteToken: "inv_signup_123",
    });
    hostedCpFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        session: {
          access_token: "cp_token_123",
        },
      }),
    });

    render(HostedOAuthCallbackPage);

    await waitFor(() => {
      expect(hostedCpFetch).toHaveBeenCalledWith(
        "account/oauth/google/finish",
        expect.objectContaining({
          method: "POST",
          body: expect.any(String),
        }),
      );
    });

    expect(JSON.parse(hostedCpFetch.mock.calls[0][1].body)).toEqual({
      code: "abc",
      state: "state_1",
      redirect_uri: `${currentPage.url.origin}/hosted/oauth/google/callback`,
      invite_token: "inv_signup_123",
    });
    await waitFor(() => {
      expect(persistHostedCpAccessToken).toHaveBeenCalledWith("cp_token_123");
      expect(loadHostedSession).toHaveBeenCalled();
      expect(mockGoto).toHaveBeenCalledWith("/hosted/onboarding/organization", {
        replaceState: true,
      });
    });
  });

  it("renders provider cancellation with signup-aware recovery copy", async () => {
    storeHostedOAuthContinuation("state_1", {
      mode: "signup",
      next: "",
      workspaceSlug: "",
      workspaceId: "",
      returnPath: "/",
      inviteToken: "inv_signup_123",
    });
    currentPage = {
      url: new URL(
        "/hosted/oauth/google/callback?error=access_denied&state=state_1",
        window.location.origin,
      ),
      params: { provider: "google" },
    };
    window.history.replaceState(
      {},
      "",
      `${currentPage.url.pathname}${currentPage.url.search}`,
    );

    const { container } = render(HostedOAuthCallbackPage);

    await waitFor(() => {
      const alert = container.querySelector("[role='alert']");
      expect(alert?.textContent).toContain(
        "Signup was canceled at the identity provider.",
      );
    });
    expect(hostedCpFetch).not.toHaveBeenCalled();
    expect(container.textContent).toContain("Back to signup");
  });

  it("keeps signup continuation available when oauth finish fails", async () => {
    storeHostedOAuthContinuation("state_1", {
      mode: "signup",
      next: "/hosted/dashboard",
      workspaceSlug: "acme",
      workspaceId: "ws_123",
      returnPath: "/threads/1",
      inviteToken: "inv_signup_123",
    });
    hostedCpFetch.mockResolvedValueOnce({
      ok: false,
      status: 401,
      statusText: "Unauthorized",
      json: async () => ({ error: { message: "oauth_invalid_state" } }),
    });

    const { container } = render(HostedOAuthCallbackPage);

    await waitFor(() => {
      const alert = container.querySelector("[role='alert']");
      expect(alert?.textContent).toContain("oauth_invalid_state");
    });
    expect(container.textContent).toContain("Back to signup");
    const link = container.querySelector("a[href]");
    expect(link?.getAttribute("href")).toBe(
      "/hosted/signup?next=%2Fhosted%2Fdashboard&workspace=acme&workspace_id=ws_123&return_path=%2Fthreads%2F1&invite=inv_signup_123",
    );
    expect(readHostedOAuthContinuation("state_1")).toEqual(
      expect.objectContaining({
        mode: "signup",
        next: "/hosted/dashboard",
        workspaceSlug: "acme",
        workspaceId: "ws_123",
        returnPath: "/threads/1",
        inviteToken: "inv_signup_123",
      }),
    );
  });
});
