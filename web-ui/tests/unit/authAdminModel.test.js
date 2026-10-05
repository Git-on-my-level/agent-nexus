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
    slug: "m5-mbp",
    agents: [
      { id: "agent-codex", handle: "codex.m5-mbp" },
      { id: "agent-claude", handle: "claude.m5-mbp" },
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
    agent_id: "agent-codex",
    actor_id: "actor-codex",
    username: "codex.m5-mbp",
    principal_kind: "agent",
    revoked: false,
  },
  {
    agent_id: "agent-claude",
    actor_id: "actor-claude",
    username: "claude.m5-mbp",
    principal_kind: "agent",
    revoked: false,
  },
  {
    agent_id: "agent-retired",
    username: "retired.m5-mbp",
    principal_kind: "agent",
    revoked: true,
  },
];

const ADMINS = [
  {
    principal_id: "agent-codex",
    actor_id: "actor-codex",
    username: "codex.m5-mbp",
    host_slug: "m5-mbp",
    auth_admin: true,
  },
];

const AUDIT = [
  {
    event_id: "e1",
    event_type: "auth_admin_granted",
    occurred_at: "2026-03-10T09:00:00Z",
    subject_agent_id: "agent-codex",
    subject_actor_id: "actor-codex",
  },
  {
    event_id: "e0",
    event_type: "auth_admin_granted",
    occurred_at: "2026-03-02T09:00:00Z",
    subject_agent_id: "agent-codex",
  },
  {
    event_id: "e2",
    event_type: "host_enroll_approved",
    occurred_at: "2026-03-11T09:00:00Z",
    subject_agent_id: "agent-claude",
  },
];

const names = {
  "actor-maya": "Maya Chen",
  "actor-alex": "Alex Ruiz",
  "actor-codex": "Codex on m5-mbp",
};
const displayName = (principal) =>
  names[principal?.actor_id] ?? principal?.username ?? "";

describe("grantTimesFromAudit", () => {
  it("keeps the newest grant per principal and ignores other events", () => {
    const times = grantTimesFromAudit(AUDIT);
    expect(times.get("agent-codex")).toBe("2026-03-10T09:00:00Z");
    expect(times.get("actor-codex")).toBe("2026-03-10T09:00:00Z");
    expect(times.has("agent-claude")).toBe(false);
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
      ["agent", "Codex on m5-mbp", "m5-mbp", "2026-03-10T09:00:00Z"],
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
      admins: [{ principal_id: "agent-codex", username: "codex.m5-mbp" }],
      principals: PRINCIPALS,
      hosts: HOSTS,
    });
    expect(rows.find((row) => row.kind === "agent").hostSlug).toBe("m5-mbp");
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
        principalId: "agent-claude",
        username: "claude.m5-mbp",
        hostSlug: "m5-mbp",
      },
    ]);
  });

  it("offers no people, since administration is not granted to them", () => {
    expect(
      grantCandidates({ principals: PRINCIPALS, admins: [], hosts: HOSTS })
        .map((candidate) => candidate.username)
        .sort(),
    ).toEqual(["claude.m5-mbp", "codex.m5-mbp"]);
  });
});

describe("hostForTarget", () => {
  it("resolves a host from a principal id or a username", () => {
    expect(
      hostForTarget("agent-claude", { principals: PRINCIPALS, hosts: HOSTS }),
    ).toBe("m5-mbp");
    expect(
      hostForTarget("claude.m5-mbp", { principals: PRINCIPALS, hosts: HOSTS }),
    ).toBe("m5-mbp");
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
