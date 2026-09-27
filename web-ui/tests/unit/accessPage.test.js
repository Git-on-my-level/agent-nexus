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
    setMode(mode) {
      value = { ...value, data: { shellCapabilities: { mode } } };
      for (const fn of subscribers) fn(value);
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
  listHosts: vi.fn(),
  listPendingHostEnrollments: vi.fn(),
  listHostEnrollmentTokens: vi.fn(),
  approveHostEnrollment: vi.fn(),
  denyHostEnrollment: vi.fn(),
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

const PENDING = {
  id: "henr_1",
  user_code: "J6FA-N4XI",
  requested_slug: "m5-mbp",
  os_user: "david",
  hostname: "m5-mbp.local",
  discovered_adapters: ["claude", "codex"],
  adoption_names: [],
  requesting_ip: "203.0.113.17",
  status: "pending",
  expires_at: new Date(Date.now() + 8 * 60_000).toISOString(),
  created_at: new Date().toISOString(),
};

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
    coreClientMock.listHosts.mockResolvedValue({ hosts: [] });
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [PENDING],
    });
    coreClientMock.listHostEnrollmentTokens.mockResolvedValue({
      enrollment_tokens: [],
    });
    coreClientMock.approveHostEnrollment.mockResolvedValue({
      enrollment: { ...PENDING, status: "approved" },
    });
  });

  afterEach(() => {
    cleanup();
    authenticatedAgent.set(null);
    vi.clearAllMocks();
  });

  it("offers no agent invites and sends hosted people to Organizations", async () => {
    render(AccessPage, {
      props: { data: { outOfWorkspaceMode: "hosted", cliBaseUrl: "" } },
    });
    await waitFor(() => {
      expect(coreClientMock.listInvites).toHaveBeenCalled();
    });
    expect(screen.queryByLabelText("Kind")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Invite a person" }),
    ).toBeNull();
    const organizationsLink = screen.getByRole("link", {
      name: /your Organizations/i,
    });
    expect(organizationsLink.getAttribute("href")).toBe(
      "/hosted/organizations",
    );
  });

  it("invites a person with a human invite", async () => {
    pageStore.setMode("local");
    render(AccessPage, {
      props: { data: { outOfWorkspaceMode: "local", cliBaseUrl: "" } },
    });
    const invite = await screen.findByRole("button", {
      name: "Invite a person",
    });
    await fireEvent.click(invite);
    await waitFor(() => {
      expect(coreClientMock.createInvite).toHaveBeenCalledWith({
        kind: "human",
      });
    });
    expect(await screen.findByText("oinv_123")).toBeTruthy();
  });

  it("approves a host only after the code is confirmed", async () => {
    pageStore.setMode("local");
    render(AccessPage, {
      props: {
        data: {
          outOfWorkspaceMode: "local",
          cliBaseUrl: "http://127.0.0.1:8081",
        },
      },
    });
    expect(await screen.findByText("J6FA-N4XI")).toBeTruthy();
    expect(screen.getByText("203.0.113.17")).toBeTruthy();
    expect(
      screen.getByText("anx --base-url http://127.0.0.1:8081 host enroll"),
    ).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Approve…" }));
    expect(coreClientMock.approveHostEnrollment).not.toHaveBeenCalled();
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [],
    });
    await fireEvent.click(
      screen.getByRole("button", { name: "Codes match, approve" }),
    );
    await waitFor(() => {
      expect(coreClientMock.approveHostEnrollment).toHaveBeenCalledWith(
        "henr_1",
      );
    });
    expect(await screen.findByText(/Approved m5-mbp/)).toBeTruthy();
  });
});
