// @vitest-environment jsdom
import { cleanup, render, waitFor } from "@testing-library/svelte";
import { get } from "svelte/store";
import { afterEach, describe, expect, it, vi } from "vitest";

const mockGoto = vi.fn();

vi.mock("$app/navigation", () => ({
  goto: (...args) => mockGoto(...args),
  invalidate: vi.fn(),
  invalidateAll: vi.fn(),
  beforeNavigate: vi.fn(),
  afterNavigate: vi.fn(),
}));

vi.mock("$app/environment", () => ({
  browser: true,
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: (fn) => {
      fn({ url: new URL("http://localhost/hosted/onboarding/organization"), params: {} });
      return () => {};
    },
  },
}));

vi.mock("$lib/hosted/cpFetch.js", () => ({
  hostedCpFetch: vi.fn(),
}));

vi.mock("$lib/hosted/session.js", () => {
  const { writable } = require("svelte/store");
  const store = writable({
    phase: "authed",
    account: { id: "u1", email: "jane@example.com", display_name: "Jane Doe" },
    organizations: [],
    activeOrgId: "",
    error: "",
  });
  return {
    hostedSession: store,
    loadHostedSession: vi.fn(async () => {
      store.set({
        phase: "authed",
        account: { id: "u1", email: "jane@example.com", display_name: "Jane Doe" },
        organizations: [],
        activeOrgId: "",
        error: "",
      });
      return get(store);
    }),
    setActiveOrg: vi.fn(),
  };
});

import OnboardingOrgPage from "../../src/routes/hosted/onboarding/organization/+page.svelte";
import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
import { hostedSession, setActiveOrg } from "$lib/hosted/session.js";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockGoto.mockReset();
  hostedSession.set({
    phase: "authed",
    account: { id: "u1", email: "jane@example.com", display_name: "Jane Doe" },
    organizations: [],
    activeOrgId: "",
    error: "",
  });
});

describe("Onboarding organization page — route guard", () => {
  it("redirects to /hosted/dashboard when user has organizations", async () => {
    hostedSession.set({
      phase: "authed",
      account: { id: "u1", email: "jane@example.com", display_name: "Jane Doe" },
      organizations: [{ id: "org1", slug: "acme", display_name: "Acme" }],
      activeOrgId: "org1",
      error: "",
    });

    render(OnboardingOrgPage);

    await waitFor(() => {
      expect(mockGoto).toHaveBeenCalledWith("/hosted/dashboard", {
        replaceState: true,
      });
    });
  });

  it("renders the form when user has zero organizations", async () => {
    const { container } = render(OnboardingOrgPage);

    await waitFor(() => {
      const heading = container.querySelector("h1");
      expect(heading).toBeTruthy();
      expect(heading.textContent).toBe("Name your organization");
    });

    const input = container.querySelector("input[type='text']");
    expect(input).toBeTruthy();
    expect(input.value).toBe("Jane's org");
  });
});

describe("Onboarding organization page — default name derivation", () => {
  it("prefills from display_name first word", async () => {
    hostedSession.set({
      phase: "authed",
      account: { id: "u1", email: "john@example.com", display_name: "John Smith" },
      organizations: [],
      activeOrgId: "",
      error: "",
    });

    const { container } = render(OnboardingOrgPage);

    await waitFor(() => {
      const input = container.querySelector("input[type='text']");
      expect(input).toBeTruthy();
      expect(input.value).toBe("John's org");
    });
  });

  it("falls back to email local part when no display_name", async () => {
    hostedSession.set({
      phase: "authed",
      account: { id: "u1", email: "alice@company.io" },
      organizations: [],
      activeOrgId: "",
      error: "",
    });

    const { container } = render(OnboardingOrgPage);

    await waitFor(() => {
      const input = container.querySelector("input[type='text']");
      expect(input).toBeTruthy();
      expect(input.value).toBe("alice's org");
    });
  });
});

describe("Onboarding organization page — form submission", () => {
  it("POSTs to organizations endpoint and navigates to onboarding/workspace", async () => {
    const mockOrg = { id: "new-org", slug: "janes-org", display_name: "Jane's org" };
    hostedCpFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ organization: mockOrg }),
    });

    const { container } = render(OnboardingOrgPage);

    await waitFor(() => {
      const input = container.querySelector("input[type='text']");
      expect(input).toBeTruthy();
    });

    const form = container.querySelector("form");
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => {
      expect(hostedCpFetch).toHaveBeenCalledWith(
        "organizations",
        expect.objectContaining({
          method: "POST",
          body: expect.any(String),
        }),
      );
    });

    const callBody = JSON.parse(hostedCpFetch.mock.calls[0][1].body);
    expect(callBody.display_name).toBe("Jane's org");
    expect(callBody.slug).toBe("jane-s-org");
    expect(callBody.plan_tier).toBe("starter");

    await waitFor(() => {
      expect(setActiveOrg).toHaveBeenCalledWith("new-org");
      expect(mockGoto).toHaveBeenCalledWith("/hosted/onboarding/workspace");
    });
  });

  it("shows error message on submit failure and preserves typed value", async () => {
    hostedCpFetch.mockResolvedValueOnce({
      ok: false,
      status: 409,
      statusText: "Conflict",
      json: async () => ({ error: { message: "Slug already taken" } }),
    });

    const { container } = render(OnboardingOrgPage);

    await waitFor(() => {
      const input = container.querySelector("input[type='text']");
      expect(input).toBeTruthy();
    });

    const form = container.querySelector("form");
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => {
      const alert = container.querySelector("[role='alert']");
      expect(alert).toBeTruthy();
      expect(alert.textContent).toContain("Slug already taken");
    });

    const input = container.querySelector("input[type='text']");
    expect(input.value).toBe("Jane's org");
  });

  it("validates empty name", async () => {
    hostedSession.set({
      phase: "authed",
      account: { id: "u1", email: "test@test.com" },
      organizations: [],
      activeOrgId: "",
      error: "",
    });

    const { container } = render(OnboardingOrgPage);

    await waitFor(() => {
      const input = container.querySelector("input[type='text']");
      expect(input).toBeTruthy();
    });

    const input = container.querySelector("input[type='text']");
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    ).set;
    nativeInputValueSetter.call(input, "   ");
    input.dispatchEvent(new Event("input", { bubbles: true }));

    const form = container.querySelector("form");
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => {
      const alert = container.querySelector("[role='alert']");
      expect(alert).toBeTruthy();
      expect(alert.textContent).toContain("required");
    });
  });
});

describe("Onboarding organization page — inline guide copy", () => {
  it("renders Orgs vs workspaces guide exactly", async () => {
    const { container } = render(OnboardingOrgPage);

    await waitFor(() => {
      const heading = container.querySelector("h1");
      expect(heading).toBeTruthy();
    });

    const guidePanel = container.querySelector(".bg-panel.border-line");
    expect(guidePanel).toBeTruthy();

    const guideTitle = guidePanel.querySelector(".text-subtitle");
    expect(guideTitle.textContent).toBe("Orgs vs workspaces");

    const labels = guidePanel.querySelectorAll(".text-micro.uppercase.tracking-wider");
    expect(labels.length).toBe(2);
    expect(labels[0].textContent.trim()).toBe("Organization");
    expect(labels[1].textContent.trim()).toBe("Workspace");

    const fullGuideText = guidePanel.textContent.replace(/\s+/g, " ").trim();

    expect(fullGuideText).toContain("Orgs vs workspaces");
    expect(fullGuideText).toContain("Organization");
    expect(fullGuideText).toContain(
      "Your team's billing, members, and audit log. Usually one per company.",
    );
    expect(fullGuideText).toContain("Workspace");
    expect(fullGuideText).toContain(
      "A project inside the org. You can have many. We'll set up your first one next.",
    );
  });
});
