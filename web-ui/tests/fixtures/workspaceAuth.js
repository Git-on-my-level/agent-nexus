/**
 * Shared shapes for auth / workspace tests (agents, CP rows, provider mocks).
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

export const cpWorkspaceRows = {
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

export function mockLocalProvider(overrides = {}) {
  return Object.freeze({
    mode: "local",
    async resolveWorkspaceBySlug() {
      return { kind: "missing" };
    },
    async resolveWorkspaceById() {
      return { kind: "missing" };
    },
    async listWorkspacesForOrganization() {
      return [];
    },
    async beginLaunchSession() {
      return { kind: "workspace_native_login" };
    },
    async exchangeLaunchSession() {
      return {
        ok: false,
        status: 503,
        code: "control_plane_unavailable",
        message: "Self-hosted workspace has no control plane configured.",
      };
    },
    buildSignInUrl() {
      return null;
    },
    async proxyHostedApi() {
      throw new Error("proxyHostedApi is not available in local provider");
    },
    describeShellCapabilities() {
      return {
        mode: "local",
        accountPath: null,
        publicOrigin: null,
        allowsEmptyStaticCatalog: false,
      };
    },
    ...overrides,
  });
}

export function mockHostedProvider(overrides = {}) {
  return Object.freeze({
    mode: "hosted",
    async resolveWorkspaceBySlug() {
      return { kind: "missing" };
    },
    async resolveWorkspaceById() {
      return { kind: "missing" };
    },
    async listWorkspacesForOrganization() {
      return [];
    },
    async beginLaunchSession() {
      return { kind: "workspace_native_login" };
    },
    async exchangeLaunchSession() {
      return {
        ok: false,
        status: 503,
        code: "session_exchange_unreachable",
        message: "Could not reach control plane for session exchange.",
      };
    },
    buildSignInUrl({ workspaceSlug, workspaceId, returnPath } = {}) {
      const params = new URLSearchParams();
      if (workspaceSlug) params.set("workspace", workspaceSlug);
      if (workspaceId) params.set("workspace_id", workspaceId);
      if (returnPath && returnPath !== "/")
        params.set("return_path", returnPath);
      const qs = params.toString();
      return qs ? `/hosted/signin?${qs}` : "/hosted/signin";
    },
    async proxyHostedApi() {
      return new Response("ok", { status: 200 });
    },
    describeShellCapabilities() {
      return {
        mode: "hosted",
        accountPath: "/hosted/onboarding",
        publicOrigin: "https://cp.example.test",
        allowsEmptyStaticCatalog: true,
      };
    },
    ...overrides,
  });
}
