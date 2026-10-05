import { describe, expect, it } from "vitest";

import {
  buildAdminRows,
  grantCandidates,
  grantTimesFromAudit,
  hostForTarget,
} from "../../src/lib/authAdminModel.js";

const HOSTS = [
  {
    id: "host_1",
    slug: "build-runner",
    agents: [
      { id: "agent-runner-a", handle: "runner-a.build-runner" },
      { id: "agent-runner-b", handle: "runner-b.build-runner" },
    ],
  },
];

const PRINCIPALS = [
  {
    agent_id: "p-maya",
    actor_id: "actor-maya",
    username: "passkey.maya.chen.1a2b",
    principal_kind: "human",
    created_at: "2026-03-01T10:00:00Z",
    revoked: false,
  },
  {
    agent_id: "p-alex",
    actor_id: "actor-alex",
    username: "passkey.alex.0c0d",
    principal_kind: "human",
    created_at: "2026-02-01T10:00:00Z",
    revoked: false,
  },
  {
    agent_id: "p-gone",
    username: "passkey.gone",
    principal_kind: "human",
    created_at: "2026-01-01T10:00:00Z",
    revoked: true,
  },
  {
    agent_id: "agent-runner-a",
    actor_id: "actor-runner-a",
    username: "runner-a.build-runner",
    principal_kind: "agent",
    revoked: false,
  },
  {
    agent_id: "agent-runner-b",
    actor_id: "actor-runner-b",
    username: "runner-b.build-runner",
    principal_kind: "agent",
    revoked: false,
  },
  {
    agent_id: "agent-retired",
    username: "retired.build-runner",
    principal_kind: "agent",
    revoked: true,
  },
];

const ADMINS = [
  {
    principal_id: "agent-runner-a",
    actor_id: "actor-runner-a",
    username: "runner-a.build-runner",
    host_slug: "build-runner",
    auth_admin: true,
  },
];

const AUDIT = [
  {
    event_id: "e1",
    event_type: "auth_admin_granted",
    occurred_at: "2026-03-10T09:00:00Z",
    subject_agent_id: "agent-runner-a",
    subject_actor_id: "actor-runner-a",
  },
  {
    event_id: "e0",
    event_type: "auth_admin_granted",
    occurred_at: "2026-03-02T09:00:00Z",
    subject_agent_id: "agent-runner-a",
  },
  {
    event_id: "e2",
    event_type: "host_enroll_approved",
    occurred_at: "2026-03-11T09:00:00Z",
    subject_agent_id: "agent-runner-b",
  },
];

const names = {
  "actor-maya": "Maya Chen",
  "actor-alex": "Alex Ruiz",
  "actor-runner-a": "Runner A on build-runner",
};
const displayName = (principal) =>
  names[principal?.actor_id] ?? principal?.username ?? "";

describe("grantTimesFromAudit", () => {
  it("keeps the newest grant per principal and ignores other events", () => {
    const times = grantTimesFromAudit(AUDIT);
    expect(times.get("agent-runner-a")).toBe("2026-03-10T09:00:00Z");
    expect(times.has("agent-runner-b")).toBe(false);
  });

  it("keys grants by principal, never by actor", () => {
    // One actor can own several agent principals on different hosts, so an
    // actor key would report one principal's grant date as another's.
    const times = grantTimesFromAudit(AUDIT);
    expect(times.has("actor-runner-a")).toBe(false);
  });

  it("gives no date to a sibling principal of an actor that was granted", () => {
    const rows = buildAdminRows({
      admins: [
        {
          principal_id: "agent-sibling",
          actor_id: "actor-runner-a",
          username: "runner-c.other-host",
        },
      ],
      principals: [],
      hosts: [],
      auditEvents: AUDIT,
    });
    expect(rows[0].grantedAt).toBe("");
  });

  it("tolerates a missing or non-array audit list", () => {
    expect(grantTimesFromAudit().size).toBe(0);
    expect(grantTimesFromAudit(null).size).toBe(0);
  });
});

describe("buildAdminRows", () => {
  it("lists people before granted agents, with host and grant date", () => {
    const rows = buildAdminRows({
      admins: ADMINS,
      principals: PRINCIPALS,
      hosts: HOSTS,
      auditEvents: AUDIT,
      currentPrincipalId: "p-maya",
      displayName,
    });
    expect(
      rows.map((row) => [row.kind, row.name, row.hostSlug, row.grantedAt]),
    ).toEqual([
      ["human", "Alex Ruiz", "", "2026-02-01T10:00:00Z"],
      ["human", "Maya Chen", "", "2026-03-01T10:00:00Z"],
      [
        "agent",
        "Runner A on build-runner",
        "build-runner",
        "2026-03-10T09:00:00Z",
      ],
    ]);
    expect(rows.find((row) => row.name === "Maya Chen").isYou).toBe(true);
    expect(rows.find((row) => row.name === "Alex Ruiz").isYou).toBe(false);
  });

  it("leaves revoked people out and never offers to revoke implicit authority", () => {
    const rows = buildAdminRows({
      admins: ADMINS,
      principals: PRINCIPALS,
      hosts: HOSTS,
      displayName,
    });
    expect(rows.some((row) => row.handle === "passkey.gone")).toBe(false);
    expect(
      rows.filter((row) => row.kind === "human").every((row) => !row.revocable),
    ).toBe(true);
    expect(
      rows.filter((row) => row.kind === "agent").every((row) => row.revocable),
    ).toBe(true);
  });

  it("falls back to the grant's own username and host when the principal is unknown", () => {
    const rows = buildAdminRows({
      admins: [{ principal_id: "agent-elsewhere", username: "fleet.host-a" }],
      principals: [],
      hosts: [],
    });
    expect(rows).toEqual([
      {
        key: "agent:agent-elsewhere",
        principalId: "agent-elsewhere",
        kind: "agent",
        name: "fleet.host-a",
        handle: "fleet.host-a",
        hostSlug: "",
        grantedAt: "",
        isYou: false,
        revocable: true,
      },
    ]);
  });

  it("omits a grant date that is outside the loaded audit window", () => {
    const rows = buildAdminRows({
      admins: ADMINS,
      principals: PRINCIPALS,
      hosts: HOSTS,
      auditEvents: [],
      displayName,
    });
    expect(rows.find((row) => row.kind === "agent").grantedAt).toBe("");
  });

  it("recovers the host from the roster when the grant omits it", () => {
    const rows = buildAdminRows({
      admins: [
        { principal_id: "agent-runner-a", username: "runner-a.build-runner" },
      ],
      principals: PRINCIPALS,
      hosts: HOSTS,
    });
    expect(rows.find((row) => row.kind === "agent").hostSlug).toBe(
      "build-runner",
    );
  });
});

describe("grantCandidates", () => {
  it("offers active agents that do not already hold administration", () => {
    expect(
      grantCandidates({
        principals: PRINCIPALS,
        admins: ADMINS,
        hosts: HOSTS,
      }),
    ).toEqual([
      {
        principalId: "agent-runner-b",
        username: "runner-b.build-runner",
        hostSlug: "build-runner",
      },
    ]);
  });

  it("offers no people, since administration is not granted to them", () => {
    expect(
      grantCandidates({ principals: PRINCIPALS, admins: [], hosts: HOSTS })
        .map((candidate) => candidate.username)
        .sort(),
    ).toEqual(["runner-a.build-runner", "runner-b.build-runner"]);
  });
});

describe("hostForTarget", () => {
  it("resolves a host from a principal id or a username", () => {
    expect(
      hostForTarget("agent-runner-b", { principals: PRINCIPALS, hosts: HOSTS }),
    ).toBe("build-runner");
    expect(
      hostForTarget("runner-b.build-runner", {
        principals: PRINCIPALS,
        hosts: HOSTS,
      }),
    ).toBe("build-runner");
  });

  it("returns nothing for an agent on no known host", () => {
    expect(
      hostForTarget("someone-else", { principals: [], hosts: HOSTS }),
    ).toBe("");
    expect(hostForTarget("", { principals: PRINCIPALS, hosts: HOSTS })).toBe(
      "",
    );
  });
});
