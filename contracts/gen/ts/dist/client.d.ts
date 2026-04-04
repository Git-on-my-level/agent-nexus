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
    artifactsCreate(options?: RequestOptions): Promise<InvokeResult>;
    artifactsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    artifactsList(options?: RequestOptions): Promise<InvokeResult>;
    boardsCardsCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    boardsCardsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    boardsCardsList(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    boardsCreate(options?: RequestOptions): Promise<InvokeResult>;
    boardsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    boardsList(options?: RequestOptions): Promise<InvokeResult>;
    boardsPatch(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    boardsWorkspace(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    cardsArchive(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    cardsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    cardsList(options?: RequestOptions): Promise<InvokeResult>;
    cardsMove(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    cardsPatch(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    cardsPurge(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    cardsRestore(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    docsCreate(options?: RequestOptions): Promise<InvokeResult>;
    docsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    docsList(options?: RequestOptions): Promise<InvokeResult>;
    docsRevisionsCreate(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    docsRevisionsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    docsRevisionsList(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    eventsCreate(options?: RequestOptions): Promise<InvokeResult>;
    eventsList(options?: RequestOptions): Promise<InvokeResult>;
    inboxAcknowledge(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    inboxList(options?: RequestOptions): Promise<InvokeResult>;
    metaHealth(options?: RequestOptions): Promise<InvokeResult>;
    metaReadyz(options?: RequestOptions): Promise<InvokeResult>;
    metaVersion(options?: RequestOptions): Promise<InvokeResult>;
    packetsReceiptsCreate(options?: RequestOptions): Promise<InvokeResult>;
    packetsReviewsCreate(options?: RequestOptions): Promise<InvokeResult>;
    packetsWorkOrdersCreate(options?: RequestOptions): Promise<InvokeResult>;
    threadsContext(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    threadsInspect(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    threadsList(options?: RequestOptions): Promise<InvokeResult>;
    threadsTimeline(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    threadsWorkspace(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    topicsCreate(options?: RequestOptions): Promise<InvokeResult>;
    topicsGet(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    topicsList(options?: RequestOptions): Promise<InvokeResult>;
    topicsPatch(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    topicsTimeline(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
    topicsWorkspace(pathParams: Record<string, string>, options?: RequestOptions): Promise<InvokeResult>;
}
