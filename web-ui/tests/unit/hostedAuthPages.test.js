// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

let currentPage = {
  url: new URL(
    "/hosted/signin?workspace=acme&workspace_id=ws_123",
    window.location.origin,
  ),
  params: {},
};

vi.mock("$app/navigation", () => ({
  goto: vi.fn(),
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
}));

import HostedSigninPage from "../../src/routes/hosted/signin/+page.svelte";
import HostedSignupPage from "../../src/routes/hosted/signup/+page.svelte";

describe("hosted auth pages", () => {
  beforeEach(() => {
    currentPage = {
      url: new URL(
        "/hosted/signin?workspace=acme&workspace_id=ws_123",
        window.location.origin,
      ),
      params: {},
    };
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("keeps hosted sign-in oauth-only with no dev shortcut or passkey copy", () => {
    const { container } = render(HostedSigninPage);

    expect(container.textContent).toContain(
      "Hosted sign-in uses Google or GitHub only.",
    );
    expect(container.textContent).not.toContain("Local dev only");
    expect(container.textContent).not.toContain("passkey");
    expect(container.textContent).toContain("Continue with Google");
    expect(container.textContent).toContain("Continue with GitHub");
  });

  it("keeps hosted signup oauth-first while preserving invite entry", () => {
    currentPage = {
      url: new URL("/hosted/signup?invite=inv_123", window.location.origin),
      params: {},
    };
    const { container } = render(HostedSignupPage);

    expect(container.textContent).toContain(
      "Hosted signup is Google or GitHub first.",
    );
    expect(container.textContent).not.toContain("passkey");
    expect(container.textContent).toContain("Invite token");
    expect(container.textContent).toContain("Continue with Google");
    expect(container.textContent).toContain("Continue with GitHub");
  });
});
