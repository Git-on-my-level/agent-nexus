// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { get } from "svelte/store";
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
  listAuthAdmins: vi.fn(),
  grantAuthAdmin: vi.fn(),
  revokeAuthAdmin: vi.fn(),
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
import {
  pendingAccessCount,
  resetPendingAccessCount,
} from "../../src/lib/pendingAccessCount.js";
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
    coreClientMock.listAuthAdmins.mockResolvedValue({ admins: [] });
    coreClientMock.grantAuthAdmin.mockResolvedValue({});
    coreClientMock.revokeAuthAdmin.mockResolvedValue({});
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
    resetPendingAccessCount();
    vi.clearAllMocks();
  });

  it("lets a person explicitly grant and revoke agent administration", async () => {
    coreClientMock.listAuthAdmins.mockResolvedValue({
      admins: [
        {
          principal_id: "agent-fleet",
          username: "fleet.host-a",
          auth_admin: true,
        },
      ],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    const input = await screen.findByLabelText(
      "Agent username or principal ID",
    );
    await fireEvent.input(input, { target: { value: "codex.host-a" } });
    await fireEvent.submit(input.closest("form"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain(
      "every process that can read the shared key",
    );
    expect(dialog.textContent).toContain(
      "Principal and human invitation revocation require a person",
    );
    expect(dialog.textContent).toContain(
      "decide host enrollments, manage enrollment tokens, revoke other hosts, and read inventory and audit",
    );
    await fireEvent.click(
      [...dialog.querySelectorAll("button")].find(
        (b) => b.textContent.trim() === "Grant administration",
      ),
    );
    await waitFor(() =>
      expect(coreClientMock.grantAuthAdmin).toHaveBeenCalledWith(
        "codex.host-a",
      ),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await fireEvent.click(
      screen.getByRole("button", { name: "Revoke administration" }),
    );
    const revokeDialog = await screen.findByRole("dialog");
    await fireEvent.click(
      [...revokeDialog.querySelectorAll("button")].find(
        (b) => b.textContent.trim() === "Revoke administration",
      ),
    );
    await waitFor(() =>
      expect(coreClientMock.revokeAuthAdmin).toHaveBeenCalledWith(
        "agent-fleet",
      ),
    );
  });

  it("shows grants without offering changes to an agent", async () => {
    authenticatedAgent.set({
      agent_id: "agent-fleet",
      principal_kind: "agent",
    });
    coreClientMock.listAuthAdmins.mockResolvedValue({
      admins: [
        {
          principal_id: "agent-fleet",
          username: "fleet.host-a",
          auth_admin: true,
        },
      ],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    await screen.findByText("fleet.host-a");
    expect(
      screen.queryByRole("button", { name: "Grant administration" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Revoke administration" }),
    ).toBeNull();
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
      name: /your account/i,
    });
    expect(organizationsLink.getAttribute("href")).toBe("/");
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

  it("lets a human cancel an approved ceremony awaiting completion", async () => {
    pageStore.setMode("local");
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [{ ...PENDING, status: "approved" }],
    });
    coreClientMock.denyHostEnrollment.mockResolvedValue({
      enrollment: { ...PENDING, status: "denied" },
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    expect(
      await screen.findByRole("button", {
        name: "Approved, awaiting completion",
      }),
    ).toHaveProperty("disabled", true);
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [],
    });
    await fireEvent.click(
      screen.getByRole("button", { name: "Cancel approval" }),
    );
    await waitFor(() => {
      expect(coreClientMock.denyHostEnrollment).toHaveBeenCalledWith("henr_1");
    });
    expect(coreClientMock.approveHostEnrollment).not.toHaveBeenCalled();
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

  it("lists people and granted agents together, with host and grant date", async () => {
    coreClientMock.listPrincipals.mockResolvedValue({
      principals: [
        {
          agent_id: "agent-human-admin",
          actor_id: "actor-human-admin",
          username: "admin@example.com",
          principal_kind: "human",
          created_at: "2026-03-01T10:00:00Z",
          revoked: false,
        },
        {
          agent_id: "agent-fleet",
          actor_id: "actor-fleet",
          username: "fleet.host-a",
          principal_kind: "agent",
          revoked: false,
        },
      ],
      active_human_principal_count: 1,
    });
    coreClientMock.listAuthAdmins.mockResolvedValue({
      admins: [
        {
          principal_id: "agent-fleet",
          actor_id: "actor-fleet",
          username: "fleet.host-a",
          host_slug: "host-a",
          auth_admin: true,
        },
      ],
    });
    coreClientMock.listAuthAudit.mockResolvedValue({
      events: [
        {
          event_id: "authevt_1",
          event_type: "auth_admin_granted",
          occurred_at: "2026-03-14T09:30:00Z",
          subject_agent_id: "agent-fleet",
        },
      ],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });

    const admins = await screen.findByRole("region", {
      name: /^Administrators/,
    });
    // The person holds administration implicitly; the agent holds a grant.
    let rows = [];
    await waitFor(() => {
      rows = [...admins.querySelectorAll("[data-auth-admin]")];
      expect(rows.map((row) => row.dataset.authAdmin)).toEqual([
        "agent-human-admin",
        "agent-fleet",
      ]);
    });
    expect(rows[0].textContent).toContain("admin@example.com");
    expect(rows[0].textContent).toContain("Person");
    expect(rows[0].textContent).toContain("admin since joining");
    expect(rows[1].textContent).toContain("fleet.host-a");
    expect(rows[1].textContent).toContain("Agent");
    expect(rows[1].textContent).toContain("host-a");
    expect(rows[1].textContent).toMatch(/since .*2026/);
    expect(admins.textContent).toContain("2");
  });

  it("omits an agent's grant date rather than guessing when audit does not reach it", async () => {
    coreClientMock.listAuthAdmins.mockResolvedValue({
      admins: [{ principal_id: "agent-fleet", username: "fleet.host-a" }],
    });
    coreClientMock.listAuthAudit.mockResolvedValue({ events: [] });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    await screen.findByText("fleet.host-a");
    const row = document.querySelector('[data-auth-admin="agent-fleet"]');
    expect(row.textContent).not.toContain("since");
  });

  it("says administration is not visible instead of showing an error on 403", async () => {
    const refused = new Error("forbidden");
    refused.status = 403;
    coreClientMock.listAuthAdmins.mockRejectedValue(refused);
    coreClientMock.listPendingHostEnrollments.mockRejectedValue(refused);
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    expect(
      await screen.findByText(
        "Only workspace administrators can see who administers this workspace.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Only workspace administrators can see access requests.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    // No badge for a reader who could not act on the number anyway.
    await waitFor(() =>
      expect(get(pendingAccessCount)).toEqual({
        workspace: "main",
        count: null,
        forbidden: true,
      }),
    );
  });

  it("publishes the pending decision count for the shell badge", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [PENDING, { ...PENDING, id: "henr_2", status: "approved" }],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    // Two rows wait, but only one of them waits on the reader.
    await waitFor(() => expect(get(pendingAccessCount).count).toBe(1));
    expect(get(pendingAccessCount).workspace).toBe("main");
    await waitFor(() =>
      expect(
        document
          .querySelector("[data-pending-access-count]")
          ?.textContent?.trim(),
      ).toBe("1"),
    );
  });
});
