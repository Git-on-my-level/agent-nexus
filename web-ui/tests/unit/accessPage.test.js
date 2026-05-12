// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/svelte";
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
    expect(screen.getByText(/hosted team management/i)).toBeTruthy();
  });
});
