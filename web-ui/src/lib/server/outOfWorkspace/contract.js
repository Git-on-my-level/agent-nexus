/**
 * @typedef {"local" | "hosted"} OutOfWorkspaceMode
 */

/**
 * @typedef {{
 *   organizationSlug: string,
 *   slug: string,
 *   label: string,
 *   description: string,
 *   coreBaseUrl: string,
 *   publicOrigin: string,
 *   id: string,
 *   workspaceId: string,
 *   organizationId: string,
 *   status: string,
 *   desiredState: string,
 * }} CatalogEntry
 */

/**
 * @typedef {{ kind: "redirect", finishUrl: string }} LaunchInstructionRedirect
 * @typedef {{ kind: "needs_signin", signInUrl: string }} LaunchInstructionNeedsSignin
 * @typedef {{ kind: "workspace_native_login" }} LaunchInstructionWorkspaceNativeLogin
 * @typedef {LaunchInstructionRedirect | LaunchInstructionNeedsSignin | LaunchInstructionWorkspaceNativeLogin} LaunchInstruction
 */

/**
 * @typedef {{
 *   workspaceId: string,
 *   exchangeToken: string,
 *   state: string,
 * }} SessionExchangeRequest
 */

/**
 * @typedef {{ ok: true, assertion: string }} SessionExchangeOk
 * @typedef {{ ok: false, status: number, code: string, message: string }} SessionExchangeFailure
 * @typedef {SessionExchangeOk | SessionExchangeFailure} SessionExchangeResult
 */

/**
 * @typedef {{ kind: "found", workspace: CatalogEntry }} ResolveResultFound
 * @typedef {{ kind: "missing" }} ResolveResultMissing
 * @typedef {{ kind: "unauthenticated" }} ResolveResultUnauthenticated
 * @typedef {ResolveResultFound | ResolveResultMissing | ResolveResultUnauthenticated} ResolveResult
 */

/**
 * @typedef {{
 *   mode: OutOfWorkspaceMode,
 *   resolveWorkspaceBySlug(args: {
 *     event: import("@sveltejs/kit").RequestEvent,
 *     organizationSlug: string,
 *     workspaceSlug: string,
 *   }): Promise<ResolveResult>,
 *   resolveWorkspaceById(args: {
 *     event: import("@sveltejs/kit").RequestEvent,
 *     workspaceId: string,
 *   }): Promise<ResolveResult>,
 *   listWorkspacesForOrganization(args: {
 *     event: import("@sveltejs/kit").RequestEvent,
 *     organizationId: string,
 *   }): Promise<CatalogEntry[]>,
 *   beginLaunchSession(args: {
 *     event: import("@sveltejs/kit").RequestEvent,
 *     workspaceId: string,
 *     returnPath: string,
 *   }): Promise<LaunchInstruction>,
 *   exchangeLaunchSession(args: {
 *     event: import("@sveltejs/kit").RequestEvent,
 *     request: SessionExchangeRequest,
 *   }): Promise<SessionExchangeResult>,
 *   buildSignInUrl(args: {
 *     workspaceSlug?: string,
 *     workspaceId?: string,
 *     returnPath?: string,
 *   }): string | null,
 *   proxyHostedApi(args: {
 *     event: import("@sveltejs/kit").RequestEvent,
 *     method: string,
 *     subpath: string,
 *   }): Promise<Response>,
 *   describeShellCapabilities(): {
 *     mode: OutOfWorkspaceMode,
 *     accountPath: string | null,
 *     publicOrigin: string | null,
 *     allowsEmptyStaticCatalog: boolean,
 *   },
 * }} OutOfWorkspaceProvider
 */

export {};
