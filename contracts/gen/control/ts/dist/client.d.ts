export type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
export interface Example {
    title: string;
    command: string;
    description?: string;
}
export interface CommandSpec {
    command_id: string;
    cli_path: string;
    method: HttpMethod;
    path: string;
    operation_id: string;
    summary?: string;
    description?: string;
    why?: string;
    group?: string;
    path_params?: string[];
    input_mode?: string;
    streaming?: unknown;
    output_envelope?: string;
    error_codes?: string[];
    stability?: string;
    surface?: string;
    agent_notes?: string;
    concepts?: string[];
    adjacent_commands?: string[];
    examples?: Example[];
    go_method: string;
    ts_method: string;
}
export interface RequestOptions {
    query?: Record<string, string | number | boolean | Array<string | number | boolean> | undefined>;
    headers?: Record<string, string>;
    body?: unknown;
}
export interface InvokeResult {
    status: number;
    headers: Headers;
    body: string;
}
export declare const commandRegistry: CommandSpec[];
export declare class OarClient {
    private readonly baseUrl;
    private readonly fetchFn;
    constructor(baseUrl: string, fetchFn?: typeof fetch);
    invoke(commandId: string, pathParams?: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlAccountsPasskeysRegisterFinish(options?: RequestOptions): Promise<InvokeResult>;
    controlAccountsPasskeysRegisterStart(options?: RequestOptions): Promise<InvokeResult>;
    controlAccountsSessionsFinish(options?: RequestOptions): Promise<InvokeResult>;
    controlAccountsSessionsRevokeCurrent(options?: RequestOptions): Promise<InvokeResult>;
    controlAccountsSessionsStart(options?: RequestOptions): Promise<InvokeResult>;
    controlBillingWebhooksStripeReceive(options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsBillingCheckoutSessionCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsBillingCustomerPortalSessionCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsBillingGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsCreate(options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsInvitesCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsInvitesList(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsInvitesRevoke(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsList(options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsMembershipsList(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsMembershipsUpdate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsUpdate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsUsageSummaryGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlOrganizationsWorkspaceInventoryList(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlProvisioningJobsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesBackupsCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesCreate(options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesDecommission(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesHeartbeatRecord(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesLaunchSessionsCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesList(options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesReplace(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesRestore(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesRestoreDrillsCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesResume(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesRoutingManifestGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesSessionExchangeCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesSuspend(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    controlWorkspacesUpgradeCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
}
