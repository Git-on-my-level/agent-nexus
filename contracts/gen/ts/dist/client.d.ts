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
    actorsList(options?: RequestOptions): Promise<InvokeResult>;
    actorsRegister(options?: RequestOptions): Promise<InvokeResult>;
    agentsMeGet(options?: RequestOptions): Promise<InvokeResult>;
    agentsMeKeysRotate(options?: RequestOptions): Promise<InvokeResult>;
    agentsMePatch(options?: RequestOptions): Promise<InvokeResult>;
    agentsMeRevoke(options?: RequestOptions): Promise<InvokeResult>;
    artifactsContentGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    artifactsCreate(options?: RequestOptions): Promise<InvokeResult>;
    artifactsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    artifactsList(options?: RequestOptions): Promise<InvokeResult>;
    authAgentsRegister(options?: RequestOptions): Promise<InvokeResult>;
    authPasskeyLoginOptions(options?: RequestOptions): Promise<InvokeResult>;
    authPasskeyLoginVerify(options?: RequestOptions): Promise<InvokeResult>;
    authPasskeyRegisterOptions(options?: RequestOptions): Promise<InvokeResult>;
    authPasskeyRegisterVerify(options?: RequestOptions): Promise<InvokeResult>;
    authToken(options?: RequestOptions): Promise<InvokeResult>;
    commitmentsCreate(options?: RequestOptions): Promise<InvokeResult>;
    commitmentsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    commitmentsList(options?: RequestOptions): Promise<InvokeResult>;
    commitmentsPatch(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    derivedRebuild(options?: RequestOptions): Promise<InvokeResult>;
    docsCreate(options?: RequestOptions): Promise<InvokeResult>;
    docsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    docsHistory(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    docsRevisionGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    docsUpdate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    eventsCreate(options?: RequestOptions): Promise<InvokeResult>;
    eventsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    eventsStream(options?: RequestOptions): Promise<InvokeResult>;
    inboxAck(options?: RequestOptions): Promise<InvokeResult>;
    inboxGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    inboxList(options?: RequestOptions): Promise<InvokeResult>;
    inboxStream(options?: RequestOptions): Promise<InvokeResult>;
    metaCommandsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    metaCommandsList(options?: RequestOptions): Promise<InvokeResult>;
    metaConceptsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    metaConceptsList(options?: RequestOptions): Promise<InvokeResult>;
    metaHandshake(options?: RequestOptions): Promise<InvokeResult>;
    metaHealth(options?: RequestOptions): Promise<InvokeResult>;
    metaVersion(options?: RequestOptions): Promise<InvokeResult>;
    packetsReceiptsCreate(options?: RequestOptions): Promise<InvokeResult>;
    packetsReviewsCreate(options?: RequestOptions): Promise<InvokeResult>;
    packetsWorkOrdersCreate(options?: RequestOptions): Promise<InvokeResult>;
    snapshotsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    threadsContext(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    threadsCreate(options?: RequestOptions): Promise<InvokeResult>;
    threadsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    threadsList(options?: RequestOptions): Promise<InvokeResult>;
    threadsPatch(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    threadsTimeline(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
}
