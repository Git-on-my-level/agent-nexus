/**
 * Shared shapes for auth / workspace tests (agents, CP rows, capability modes).
 */

export const agents = {
  workspaceHumanAgentIdOnly: {
    agent_id: "ag-ws-1",
    actor_id: "",
    username: "human@example.com",
    principal_kind: "human",
    auth_method: "passkey",
  },
  workspaceHumanFull: {
    agent_id: "ag-ws-2",
    actor_id: "actor-ws-2",
    username: "human@example.com",
    principal_kind: "human",
    auth_method: "passkey",
  },
  devActorUnauthenticated: {
    agent_id: "",
    actor_id: "",
    username: "",
    principal_kind: "human",
    auth_method: "none",
  },
};

export const controlPlaneWorkspaceRows = {
  minimal: {
    id: "ws-cp-1",
    slug: "alpha",
    organization_slug: "acme",
    display_name: "Alpha",
    description: "",
    core_origin: "http://127.0.0.1:9000",
    public_origin: "",
    status: "active",
    desired_state: "running",
  },
  full: {
    id: "ws-cp-2",
    slug: "beta",
    organization_slug: "acme",
    organization_id: "org-1",
    display_name: "Beta",
    description: "Test workspace",
    core_origin: "http://127.0.0.1:9001",
    public_origin: "https://beta.example",
    status: "active",
    desired_state: "running",
  },
};

export const capabilities = {
  hosted: { mode: "hosted", supportsCpWorkspaceIdLookup: false },
  packedHostDev: { mode: "packed-host-dev", supportsCpWorkspaceIdLookup: true },
  local: { mode: "local", supportsCpWorkspaceIdLookup: false },
};
