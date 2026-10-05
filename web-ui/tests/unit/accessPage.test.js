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
  listAccessRequests: vi.fn(),
  approveAccessRequest: vi.fn(),
  denyAccessRequest: vi.fn(),
  getAccessSummary: vi.fn(),
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
  requested_slug: "workstation-a",
  os_user: "operator",
  hostname: "workstation-a.local",
  discovered_adapters: ["claude", "codex"],
  adoption_names: [],
  requesting_ip: "203.0.113.17",
  status: "pending",
  expires_at: new Date(Date.now() + 8 * 60_000).toISOString(),
  created_at: new Date().toISOString(),
};

const ACCESS_REQUEST = {
  id: "areq_1",
  principal_id: "agent-fleet",
  actor_id: "actor-fleet",
  username: "fleet.host-a",
  grant: "auth-admin",
  reason: "ship the release",
  status: "pending",
  created_at: new Date(Date.now() - 90_000).toISOString(),
  request_event_ref: "event:evt_1",
  inbox_item_id: "inbox_1",
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
    coreClientMock.listAccessRequests.mockResolvedValue({ requests: [] });
    coreClientMock.approveAccessRequest.mockResolvedValue({});
    coreClientMock.denyAccessRequest.mockResolvedValue({});
    coreClientMock.getAccessSummary.mockResolvedValue({ pending_count: 0 });
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
    expect(await screen.findByText(/Approved workstation-a/)).toBeTruthy();
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
    expect(rows[1].textContent).toMatch(/admin since .*2026/);
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

  it("says access is not yours to manage, once, when every read is refused", async () => {
    const refused = new Error("forbidden");
    refused.status = 403;
    for (const call of [
      "listHosts",
      "listPendingHostEnrollments",
      "listAccessRequests",
      "listHostEnrollmentTokens",
      "listAuthAdmins",
      "listPrincipals",
      "listInvites",
      "listAuthAudit",
    ]) {
      coreClientMock[call].mockRejectedValue(refused);
    }
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    expect(
      await screen.findByText(
        "Only workspace administrators can manage access.",
      ),
    ).toBeTruthy();
    // A refusal is an answer, not a fault, and the page must not then go on
    // to describe a workspace the reader was not allowed to see.
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByText("No machines enrolled yet")).toBeNull();
    expect(screen.queryByText("No people yet.")).toBeNull();
    expect(screen.queryByText("No access events yet.")).toBeNull();
    expect(
      screen.queryByText(/Last step: enroll the machine your agents run on/),
    ).toBeNull();
    expect(document.querySelector("[data-auth-admin]")).toBeNull();
    // No badge for a reader who could not act on the number anyway.
    await waitFor(() =>
      expect(get(pendingAccessCount)).toEqual({
        workspace: "main",
        count: null,
        forbidden: true,
      }),
    );
  });

  it("keeps a refused host read from claiming the workspace has no machines", async () => {
    const refused = new Error("forbidden");
    refused.status = 403;
    coreClientMock.listHosts.mockRejectedValue(refused);
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    // Pending requests still load, so this is not the all-refused case.
    expect(await screen.findByText("J6FA-N4XI")).toBeTruthy();
    expect(screen.queryByText("No machines enrolled yet")).toBeNull();
    expect(
      screen.queryByText(/Last step: enroll the machine your agents run on/),
    ).toBeNull();
  });

  it("treats a 401 as a failed read, not as a missing grant", async () => {
    // Telling a signed-out administrator they are not an administrator is
    // worse than saying the read failed.
    const expired = new Error("unauthorized");
    expired.status = 401;
    coreClientMock.listAuthAdmins.mockRejectedValue(expired);
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(
      screen.queryByText(
        "Only workspace administrators can see who administers this workspace.",
      ),
    ).toBeNull();
    expect(
      screen.queryByText("Only workspace administrators can manage access."),
    ).toBeNull();
  });

  it("offers an agent administrator no way to change a grant", async () => {
    authenticatedAgent.set({
      agent_id: "agent-fleet",
      actor_id: "actor-fleet",
      username: "fleet.host-a",
      principal_kind: "agent",
    });
    coreClientMock.listAuthAdmins.mockResolvedValue({
      admins: [{ principal_id: "agent-fleet", username: "fleet.host-a" }],
    });
    coreClientMock.listPrincipals.mockResolvedValue({
      principals: [
        {
          agent_id: "agent-other",
          username: "other.host-a",
          principal_kind: "agent",
          revoked: false,
        },
      ],
      active_human_principal_count: 1,
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    await waitFor(() =>
      expect(
        document.querySelector('[data-auth-admin="agent-fleet"]'),
      ).toBeTruthy(),
    );
    // Only a person can grant or revoke; core refuses an agent either way.
    expect(
      screen.queryByLabelText("Agent username or principal ID"),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Grant administration" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Revoke administration" }),
    ).toBeNull();
  });

  it("counts the people core reports, not just the page it returned", async () => {
    coreClientMock.listPrincipals.mockResolvedValue({
      // A fleet's agent principals can fill the newest-first page and push
      // the people who joined at setup off it entirely.
      principals: [
        {
          agent_id: "agent-fleet",
          username: "fleet.host-a",
          principal_kind: "agent",
          revoked: false,
        },
      ],
      active_human_principal_count: 6,
    });
    coreClientMock.listAuthAdmins.mockResolvedValue({
      admins: [{ principal_id: "agent-fleet", username: "fleet.host-a" }],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    const admins = await screen.findByRole("region", {
      name: /^Administrators/,
    });
    await waitFor(() => expect(admins.textContent).toContain("7"));
    expect(
      (await screen.findByText(/more people administer this workspace/))
        .textContent,
    ).toContain("6 more people");
    // Never the sentence that core's break-glass rule makes impossible.
    expect(
      screen.queryByText("Nobody administers this workspace yet."),
    ).toBeNull();
  });

  it("refuses to promise a grant that core will reject for a person", async () => {
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
      ],
      active_human_principal_count: 1,
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    const input = await screen.findByLabelText(
      "Agent username or principal ID",
    );
    await fireEvent.input(input, { target: { value: "admin@example.com" } });
    await fireEvent.submit(input.closest("form"));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(coreClientMock.grantAuthAdmin).not.toHaveBeenCalled();
    expect(
      await screen.findByText(/People already administer this workspace/),
    ).toBeTruthy();
  });

  it("publishes the same number the badge shows, over both kinds", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [
        PENDING,
        { ...PENDING, id: "henr_2", status: "approved" },
        // Expired: nobody's to decide, and core's summary drops it too.
        {
          ...PENDING,
          id: "henr_3",
          expires_at: new Date(Date.now() - 60_000).toISOString(),
        },
      ],
    });
    coreClientMock.listAccessRequests.mockResolvedValue({
      requests: [ACCESS_REQUEST],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    // One request and one pending enrollment. The approved row is waiting on
    // its machine and the expired one on nobody, so neither is a decision.
    await waitFor(() => expect(get(pendingAccessCount).count).toBe(2));
    expect(get(pendingAccessCount).workspace).toBe("main");
    // The heading cannot disagree with the badge: one number, one rule.
    await waitFor(() =>
      expect(
        document
          .querySelector("[data-pending-access-count]")
          ?.textContent?.trim(),
      ).toBe("2"),
    );
    expect(document.querySelectorAll("[data-host-enrollment]")).toHaveLength(3);
  });

  it("does not claim a decision is needed for a ceremony its machine owns", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [{ ...PENDING, status: "approved" }],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    expect(await screen.findByText("Enrollment in progress")).toBeTruthy();
    expect(document.querySelector("[data-pending-access-count]")).toBeNull();
    await waitFor(() => expect(get(pendingAccessCount).count).toBe(0));
  });

  it("lets a person grant an agent's access request after confirming", async () => {
    coreClientMock.listAccessRequests.mockResolvedValue({
      requests: [ACCESS_REQUEST],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    expect(await screen.findByText(/asks to administer access/)).toBeTruthy();
    expect(screen.getByText(/ship the release/)).toBeTruthy();

    // The section also holds an enrolling machine; act on the request row.
    const row = document.querySelector('[data-access-request="areq_1"]');
    await fireEvent.click(
      [...row.querySelectorAll("button")].find(
        (button) => button.textContent.trim() === "Approve…",
      ),
    );
    expect(coreClientMock.approveAccessRequest).not.toHaveBeenCalled();
    const confirm = row.querySelector("[data-access-request-confirm]");
    const copy = confirm.textContent.replace(/\s+/g, " ");
    expect(copy).toContain("decide host enrollments, manage enrollment tokens");
    expect(copy).toContain(
      "Principal and human invitation revocation still require a person",
    );
    // No host in the roster for this principal, so none is named: the request
    // itself carries no host.
    expect(copy).not.toContain("shared key on");
    coreClientMock.listAccessRequests.mockResolvedValue({ requests: [] });
    await fireEvent.click(
      [...confirm.querySelectorAll("button")].find(
        (button) => button.textContent.trim() === "Grant administration",
      ),
    );
    await waitFor(() =>
      expect(coreClientMock.approveAccessRequest).toHaveBeenCalledWith(
        "areq_1",
      ),
    );
    expect(coreClientMock.denyAccessRequest).not.toHaveBeenCalled();
    // Approving creates a grant, so Administrators must be re-read.
    await waitFor(() =>
      expect(coreClientMock.listAuthAdmins.mock.calls.length).toBeGreaterThan(
        1,
      ),
    );
  });

  it("denies an access request in one click and says nothing changed", async () => {
    coreClientMock.listAccessRequests.mockResolvedValue({
      requests: [ACCESS_REQUEST],
    });
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    await screen.findByText(/asks to administer access/);
    const row = document.querySelector('[data-access-request="areq_1"]');
    coreClientMock.listAccessRequests.mockResolvedValue({ requests: [] });
    await fireEvent.click(
      [...row.querySelectorAll("button")].find(
        (button) => button.textContent.trim() === "Deny",
      ),
    );
    await waitFor(() =>
      expect(coreClientMock.denyAccessRequest).toHaveBeenCalledWith("areq_1"),
    );
    expect(coreClientMock.approveAccessRequest).not.toHaveBeenCalled();
    expect(await screen.findByText(/Its access is unchanged/)).toBeTruthy();
  });

  it("tells an agent administrator that grant requests are people-only", async () => {
    // `/auth/access-requests` is human-only while the enrollment list allows
    // an auth-admin agent, so one reader can see one list and not the other.
    const refused = new Error("forbidden");
    refused.status = 403;
    authenticatedAgent.set({
      agent_id: "agent-fleet",
      actor_id: "actor-fleet",
      username: "fleet.host-a",
      principal_kind: "agent",
    });
    coreClientMock.listAccessRequests.mockRejectedValue(refused);
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    // The enrollment it may decide still shows.
    expect(await screen.findByText("J6FA-N4XI")).toBeTruthy();
    expect(
      await screen.findByText(
        "Agents asking for a grant are shown to people only.",
      ),
    ).toBeTruthy();
    // Not "ask for administration": it already has administration, and more
    // of it would still not let it read this.
    expect(
      screen.queryByText(
        "Only workspace administrators can see access requests.",
      ),
    ).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("never renders an empty section when a refused read hid the rows", async () => {
    const refused = new Error("forbidden");
    refused.status = 403;
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [],
    });
    coreClientMock.listAccessRequests.mockRejectedValue(refused);
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    // Nothing to list, but the reader must not conclude nothing is waiting.
    expect(
      await screen.findByText(
        "Agents asking for a grant are shown to people only.",
      ),
    ).toBeTruthy();
  });

  it("does not let a read that started before a decision undo it", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      coreClientMock.listAccessRequests.mockResolvedValue({
        requests: [ACCESS_REQUEST],
      });
      render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
      await screen.findByText(/asks to administer access/);
      const row = document.querySelector('[data-access-request="areq_1"]');

      // Put a poll in flight that still carries the undecided row, then
      // decide while it is out.
      let releaseStalePoll = () => {};
      coreClientMock.listAccessRequests.mockReturnValueOnce(
        new Promise((resolve) => {
          releaseStalePoll = () => resolve({ requests: [ACCESS_REQUEST] });
        }),
      );
      await vi.advanceTimersByTimeAsync(5_000);
      coreClientMock.listAccessRequests.mockResolvedValue({ requests: [] });

      await fireEvent.click(
        [...row.querySelectorAll("button")].find(
          (button) => button.textContent.trim() === "Deny",
        ),
      );
      await waitFor(() =>
        expect(coreClientMock.denyAccessRequest).toHaveBeenCalledWith("areq_1"),
      );
      releaseStalePoll();
      // The decided row must not come back with its controls live.
      await waitFor(() =>
        expect(
          document.querySelector('[data-access-request="areq_1"]'),
        ).toBeNull(),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("re-reads after a decision that reported failure", async () => {
    // Core grants and projects in separate steps, so a reported failure can
    // still have landed. The page must not take the error as the last word.
    coreClientMock.listAccessRequests.mockResolvedValue({
      requests: [ACCESS_REQUEST],
    });
    coreClientMock.approveAccessRequest.mockRejectedValue(
      new Error("the grant could not be recorded"),
    );
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    await screen.findByText(/asks to administer access/);
    const row = document.querySelector('[data-access-request="areq_1"]');
    await fireEvent.click(
      [...row.querySelectorAll("button")].find(
        (button) => button.textContent.trim() === "Approve…",
      ),
    );
    const adminReadsBefore = coreClientMock.listAuthAdmins.mock.calls.length;
    await fireEvent.click(
      [...row.querySelectorAll("button")].find(
        (button) => button.textContent.trim() === "Grant administration",
      ),
    );
    expect(
      await screen.findByText(/the grant could not be recorded/),
    ).toBeTruthy();
    await waitFor(() =>
      expect(coreClientMock.listAuthAdmins.mock.calls.length).toBeGreaterThan(
        adminReadsBefore,
      ),
    );
  });

  it("says an access request read failed rather than showing nothing", async () => {
    coreClientMock.listPendingHostEnrollments.mockResolvedValue({
      enrollments: [],
    });
    coreClientMock.listAccessRequests.mockRejectedValue(
      new Error("core unreachable"),
    );
    render(AccessPage, { props: { data: { outOfWorkspaceMode: "local" } } });
    expect(
      await screen.findByText(/Access requests did not load/),
    ).toBeTruthy();
  });
});
