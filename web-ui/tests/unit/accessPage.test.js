// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const pageStore = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/acme/w/main/access"),
    params: {
      organization: "acme",
      workspace: "main",
    },
    data: {
      shellCapabilities: {
        mode: "hosted",
      },
    },
  };
  const subscribers = new Set();
  return {
    subscribe(fn) {
      subscribers.add(fn);
      fn(value);
      return () => subscribers.delete(fn);
    },
    reset() {
      value = {
        url: new URL("http://localhost/o/acme/w/main/access"),
        params: {
          organization: "acme",
          workspace: "main",
        },
        data: {
          shellCapabilities: {
            mode: "hosted",
          },
        },
      };
      for (const fn of subscribers) fn(value);
    },
  };
});

const coreClientMock = vi.hoisted(() => ({
  listPrincipals: vi.fn(),
  listInvites: vi.fn(),
  listAuthAudit: vi.fn(),
  createInvite: vi.fn(),
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: pageStore.subscribe,
  },
}));

vi.mock("$lib/coreClient", () => ({
  coreClient: coreClientMock,
}));

import { authenticatedAgent } from "../../src/lib/authSession.js";
import AccessPage from "../../src/routes/o/[organization]/w/[workspace]/access/+page.svelte";

describe("access page", () => {
  beforeEach(() => {
    pageStore.reset();
    authenticatedAgent.set({
      agent_id: "agent-human-admin",
      actor_id: "actor-human-admin",
      username: "admin@example.com",
      principal_kind: "human",
    });
    coreClientMock.listPrincipals.mockResolvedValue({
      principals: [],
      active_human_principal_count: 1,
    });
    coreClientMock.listInvites.mockResolvedValue({ invites: [] });
    coreClientMock.listAuthAudit.mockResolvedValue({ events: [] });
    coreClientMock.createInvite.mockResolvedValue({ token: "oinv_123" });
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
    });
  });

  afterEach(() => {
    cleanup();
    authenticatedAgent.set(null);
    vi.clearAllMocks();
  });

  it("hides human and any workspace invite controls in hosted mode", async () => {
    render(AccessPage, {
      props: {
        data: {
          outOfWorkspaceMode: "hosted",
          workspaceId: "ws-main",
          registrationBaseUrl: "https://agentnexus.test/o/acme/w/main",
        },
      },
    });

    await waitFor(() => {
      expect(coreClientMock.listInvites).toHaveBeenCalled();
    });

    const kindSelect = screen.getByLabelText("Kind");
    expect(kindSelect.value).toBe("agent");
    expect(screen.getByRole("option", { name: "Agent" })).toBeTruthy();
    expect(screen.queryByRole("option", { name: "Human" })).toBeNull();
    expect(screen.queryByRole("option", { name: "Any" })).toBeNull();
    expect(screen.queryByLabelText(/Agent profile name/i)).toBeNull();
    expect(screen.getByLabelText(/Agent username/i)).toBeTruthy();
    const organizationsLink = screen.getByRole("link", {
      name: /your Organizations/i,
    });
    expect(organizationsLink.getAttribute("href")).toBe(
      "/hosted/organizations",
    );
  });

  it("requires an agent username before creating a hosted invite", async () => {
    render(AccessPage, {
      props: {
        data: {
          outOfWorkspaceMode: "hosted",
          workspaceId: "ws-main",
          registrationBaseUrl: "https://core.example.com",
        },
      },
    });

    await waitFor(() => {
      expect(coreClientMock.listInvites).toHaveBeenCalled();
    });

    const submitButton = screen.getByRole("button", {
      name: "Create invite",
    });
    await fireEvent.submit(submitButton.closest("form"));

    await waitFor(() => {
      expect(
        screen.getByText(
          "Agent username is required for hosted agent invites.",
        ),
      ).toBeTruthy();
    });
    expect(coreClientMock.createInvite).not.toHaveBeenCalled();
  });

  it("copies hosted invite instructions with the username as the default agent profile", async () => {
    render(AccessPage, {
      props: {
        data: {
          outOfWorkspaceMode: "hosted",
          workspaceId: "ws-main",
          registrationBaseUrl: "https://core.example.com",
        },
      },
    });

    await waitFor(() => {
      expect(coreClientMock.listInvites).toHaveBeenCalled();
    });

    await fireEvent.input(screen.getByLabelText(/Agent username/i), {
      target: { value: "claude-code" },
    });
    await fireEvent.click(
      screen.getByRole("button", { name: "Create invite" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Invite created successfully")).toBeTruthy();
    });

    expect(screen.queryByRole("button", { name: "Copy token" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Copy CLI command" }),
    ).toBeNull();
    await fireEvent.click(
      screen.getByRole("button", { name: "Copy instructions" }),
    );

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      expect.stringContaining(
        "anx --base-url https://core.example.com --agent claude-code auth register --username claude-code --invite-token oinv_123",
      ),
    );
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      expect.stringContaining("anx --version"),
    );
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      expect.stringContaining(
        "curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | sh",
      ),
    );
  });
});
