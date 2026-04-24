export function createLocalProvider() {
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
      const { error } = await import("@sveltejs/kit");
      throw error(404, "Not found");
    },
    describeShellCapabilities() {
      return {
        mode: "local",
        accountPath: null,
        publicOrigin: null,
        allowsEmptyStaticCatalog: false,
      };
    },
  });
}
