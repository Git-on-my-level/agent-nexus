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
      fn({
        url: new URL("http://localhost/hosted/onboarding/workspace"),
        params: {},
      });
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
    organizations: [{ id: "org1", slug: "acme", display_name: "Acme" }],
    activeOrgId: "org1",
    error: "",
  });
  return {
    hostedSession: store,
    loadHostedSession: vi.fn(async () => {
      store.set({
        phase: "authed",
        account: {
          id: "u1",
          email: "jane@example.com",
          display_name: "Jane Doe",
        },
        organizations: [{ id: "org1", slug: "acme", display_name: "Acme" }],
        activeOrgId: "org1",
        error: "",
      });
      return get(store);
    }),
    setActiveOrg: vi.fn(),
  };
});

import OnboardingWorkspacePage from "../../src/routes/hosted/onboarding/workspace/+page.svelte";
import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
import { hostedSession } from "$lib/hosted/session.js";

const emptyWorkspaceList = {
  ok: true,
  json: async () => ({ workspaces: [] }),
};

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockGoto.mockReset();
  hostedSession.set({
    phase: "authed",
    account: { id: "u1", email: "jane@example.com", display_name: "Jane Doe" },
    organizations: [{ id: "org1", slug: "acme", display_name: "Acme" }],
    activeOrgId: "org1",
    error: "",
  });
});

describe("Onboarding workspace page — route guard", () => {
  it("redirects to /hosted/start when unauthed", async () => {
    hostedSession.set({
      phase: "unauthed",
      account: null,
      organizations: [],
      activeOrgId: "",
      error: "",
    });

    render(OnboardingWorkspacePage);

    await waitFor(() => {
      expect(mockGoto).toHaveBeenCalledWith("/hosted/start", {
        replaceState: true,
      });
    });
  });

  it("redirects to /hosted/onboarding/organization when user has zero orgs", async () => {
    hostedSession.set({
      phase: "authed",
      account: { id: "u1", email: "jane@example.com" },
      organizations: [],
      activeOrgId: "",
      error: "",
    });

    render(OnboardingWorkspacePage);

    await waitFor(() => {
      expect(mockGoto).toHaveBeenCalledWith("/hosted/onboarding/organization", {
        replaceState: true,
      });
    });
  });

  it("redirects to /hosted/dashboard when user already has workspaces", async () => {
    hostedCpFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ workspaces: [{ id: "ws1", slug: "main" }] }),
    });

    render(OnboardingWorkspacePage);

    await waitFor(() => {
      expect(mockGoto).toHaveBeenCalledWith("/hosted/dashboard", {
        replaceState: true,
      });
    });
  });

  it("renders the form when user has an org but zero workspaces", async () => {
    hostedCpFetch.mockResolvedValue(emptyWorkspaceList);

    const { container } = render(OnboardingWorkspacePage);

    await waitFor(() => {
      const heading = container.querySelector("h1");
      expect(heading).toBeTruthy();
      expect(heading.textContent).toBe("Name your first workspace");
    });

    const input = container.querySelector("input[type='text']");
    expect(input).toBeTruthy();
    expect(input.value).toBe("Main");
  });
});

describe("Onboarding workspace page — form submission", () => {
  it("POSTs to workspaces endpoint and navigates to workspace inbox", async () => {
    hostedCpFetch.mockResolvedValue(emptyWorkspaceList);

    const mockWs = { id: "ws1", slug: "main", display_name: "Main" };
    hostedCpFetch.mockImplementation((path, init) => {
      if (init?.method === "POST") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ workspace: mockWs }),
        });
      }
      return Promise.resolve(emptyWorkspaceList);
    });

    const { container } = render(OnboardingWorkspacePage);

    await waitFor(() => {
      const input = container.querySelector("input[type='text']");
      expect(input).toBeTruthy();
    });

    const form = container.querySelector("form");
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );

    await waitFor(() => {
      expect(hostedCpFetch).toHaveBeenCalledWith(
        "workspaces",
        expect.objectContaining({
          method: "POST",
          body: expect.any(String),
        }),
      );
    });

    const createCall = hostedCpFetch.mock.calls.find(
      (c) => c[1]?.method === "POST",
    );
    const callBody = JSON.parse(createCall[1].body);
    expect(callBody.display_name).toBe("Main");
    expect(callBody.slug).toBe("main");
    expect(callBody.organization_id).toBe("org1");

    await waitFor(() => {
      expect(mockGoto).toHaveBeenCalledWith("/o/acme/w/main/inbox", {
        replaceState: true,
      });
    });
  });

  it("shows error message on submit failure", async () => {
    hostedCpFetch.mockImplementation((path, init) => {
      if (init?.method === "POST") {
        return Promise.resolve({
          ok: false,
          status: 409,
          statusText: "Conflict",
          json: async () => ({ error: { message: "Slug already taken" } }),
        });
      }
      return Promise.resolve(emptyWorkspaceList);
    });

    const { container } = render(OnboardingWorkspacePage);

    await waitFor(() => {
      const input = container.querySelector("input[type='text']");
      expect(input).toBeTruthy();
    });

    const form = container.querySelector("form");
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );

    await waitFor(() => {
      const alert = container.querySelector("[role='alert']");
      expect(alert).toBeTruthy();
      expect(alert.textContent).toContain("Slug already taken");
    });

    const input = container.querySelector("input[type='text']");
    expect(input.value).toBe("Main");
  });

  it("validates empty workspace name", async () => {
    hostedCpFetch.mockResolvedValue(emptyWorkspaceList);

    const { container } = render(OnboardingWorkspacePage);

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
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );

    await waitFor(() => {
      const alert = container.querySelector("[role='alert']");
      expect(alert).toBeTruthy();
      expect(alert.textContent).toContain("required");
    });
  });
});

describe("Onboarding workspace page — inline guide copy", () => {
  it("renders the guide card with expected content", async () => {
    hostedCpFetch.mockResolvedValue(emptyWorkspaceList);

    const { container } = render(OnboardingWorkspacePage);

    await waitFor(() => {
      const heading = container.querySelector("h1");
      expect(heading).toBeTruthy();
    });

    const guidePanel = container.querySelector(".bg-panel.border-line");
    expect(guidePanel).toBeTruthy();

    const fullGuideText = guidePanel.textContent.replace(/\s+/g, " ").trim();

    expect(fullGuideText).toContain(
      "One workspace per project or codebase works best.",
    );
    expect(fullGuideText).toContain("You can add more later.");
  });
});

describe("Onboarding workspace page — keyboard shortcut", () => {
  it("submits the form on Cmd+Enter", async () => {
    hostedCpFetch.mockImplementation((path, init) => {
      if (init?.method === "POST") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ workspace: { id: "ws1", slug: "main" } }),
        });
      }
      return Promise.resolve(emptyWorkspaceList);
    });

    const { container } = render(OnboardingWorkspacePage);

    await waitFor(() => {
      const input = container.querySelector("input[type='text']");
      expect(input).toBeTruthy();
    });

    const form = container.querySelector("form");
    form.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        metaKey: true,
        bubbles: true,
      }),
    );

    await waitFor(() => {
      expect(hostedCpFetch).toHaveBeenCalledWith(
        "workspaces",
        expect.objectContaining({
          method: "POST",
        }),
      );
    });
  });
});
