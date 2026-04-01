import {
  OarClient,
  commandRegistry,
} from "../../../contracts/gen/ts/dist/client.js";

import { getExpectedCommandRegistryDigest } from "./commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION, normalizeBaseUrl } from "./config.js";
import { appPath } from "./workspacePaths.js";

const commandRegistryByID = new Map(
  commandRegistry.map((command) => [command.command_id, command]),
);

function toAbsoluteUrl(baseUrl, pathWithQuery) {
  if (!baseUrl) {
    return pathWithQuery;
  }

  return new URL(pathWithQuery, `${baseUrl}/`).toString();
}

function extractErrorMessage(detailsText) {
  const raw = String(detailsText ?? "").trim();
  if (!raw) {
    return "";
  }

  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed?.error === "string") {
      return parsed.error;
    }

    if (typeof parsed?.error?.message === "string") {
      return parsed.error.message;
    }

    if (typeof parsed?.message === "string") {
      return parsed.message;
    }
  } catch {
    // Keep raw response text when payload is non-JSON.
  }

  return raw;
}

function parseJsonBody(body, commandId) {
  const raw = String(body ?? "").trim();
  if (!raw) {
    return {};
  }

  try {
    return JSON.parse(raw);
  } catch {
    throw new Error(`oar-core returned invalid JSON for ${commandId}.`);
  }
}

function renderPath(pathTemplate, pathParams = {}) {
  return String(pathTemplate).replace(/\{([^{}]+)\}/g, (_match, name) => {
    const value = pathParams[name];
    if (value === undefined || value === null || value === "") {
      throw new Error(`missing path param ${name}`);
    }
    return encodeURIComponent(String(value));
  });
}

function firstStructuredPayloadIndex(value) {
  const objectIndex = value.indexOf("{");
  const arrayIndex = value.indexOf("[");
  const indexes = [objectIndex, arrayIndex].filter((index) => index >= 0);
  return indexes.length > 0 ? Math.min(...indexes) : -1;
}

function parseGeneratedFailure(error, commandId) {
  if (!(error instanceof Error)) {
    return null;
  }

  const prefix = `request failed for ${commandId}:`;
  if (!error.message.startsWith(prefix)) {
    return null;
  }

  const rest = error.message.slice(prefix.length).trim();
  const statusMatch = rest.match(/^(\d+)\s+(.*)$/);
  if (!statusMatch) {
    return {
      status: undefined,
      details: extractErrorMessage(rest),
    };
  }

  const status = Number.parseInt(statusMatch[1], 10);
  const remainder = statusMatch[2];
  const payloadStart = firstStructuredPayloadIndex(remainder);
  const payloadText =
    payloadStart >= 0 ? remainder.slice(payloadStart) : remainder;
  const details =
    extractErrorMessage(payloadText) || extractErrorMessage(remainder);

  return {
    status: Number.isFinite(status) ? status : undefined,
    details,
  };
}

function buildQueryString(query = {}) {
  const params = new URLSearchParams();

  for (const [key, rawValue] of Object.entries(query ?? {})) {
    if (rawValue === undefined || rawValue === null || rawValue === "") {
      continue;
    }

    if (Array.isArray(rawValue)) {
      for (const item of rawValue) {
        if (item === undefined || item === null || item === "") {
          continue;
        }
        params.append(key, String(item));
      }
      continue;
    }

    params.set(key, String(rawValue));
  }

  return params.toString();
}

function parseSSEChunk(rawChunk) {
  const lines = String(rawChunk ?? "")
    .split("\n")
    .map((line) => line.trimEnd());

  let id = "";
  let event = "message";
  const dataLines = [];

  for (const line of lines) {
    if (!line || line.startsWith(":")) {
      continue;
    }

    const separatorIndex = line.indexOf(":");
    const field = separatorIndex >= 0 ? line.slice(0, separatorIndex) : line;
    let value = separatorIndex >= 0 ? line.slice(separatorIndex + 1) : "";
    if (value.startsWith(" ")) {
      value = value.slice(1);
    }

    if (field === "id") {
      id = value;
      continue;
    }
    if (field === "event") {
      event = value || event;
      continue;
    }
    if (field === "data") {
      dataLines.push(value);
    }
  }

  if (!id && dataLines.length === 0) {
    return null;
  }

  const rawData = dataLines.join("\n");
  let data = rawData;
  if (rawData) {
    try {
      data = JSON.parse(rawData);
    } catch {
      data = rawData;
    }
  }

  return { id, event, data };
}

async function consumeSSEStream(response, { onEvent, signal } = {}) {
  if (!response.body) {
    throw new Error("oar-core returned an empty event stream response body.");
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    while (true) {
      if (signal?.aborted) {
        throw new DOMException("The operation was aborted.", "AbortError");
      }

      const { done, value } = await reader.read();
      if (done) {
        break;
      }

      buffer += decoder.decode(value, { stream: true });
      buffer = buffer.replace(/\r\n/g, "\n").replace(/\r/g, "\n");

      let separatorIndex = buffer.indexOf("\n\n");
      while (separatorIndex >= 0) {
        const rawChunk = buffer.slice(0, separatorIndex);
        buffer = buffer.slice(separatorIndex + 2);
        const parsed = parseSSEChunk(rawChunk);
        if (parsed) {
          await onEvent?.(parsed);
        }
        separatorIndex = buffer.indexOf("\n\n");
      }
    }

    buffer += decoder.decode();
    const trailing = parseSSEChunk(
      buffer.replace(/\r\n/g, "\n").replace(/\r/g, "\n"),
    );
    if (trailing) {
      await onEvent?.(trailing);
    }
  } finally {
    reader.releaseLock();
  }
}

function normalizeRequestError(error, { target, commandId, method, path }) {
  const generatedFailure = parseGeneratedFailure(error, commandId);

  if (generatedFailure) {
    const detailSuffix = generatedFailure.details
      ? ` - ${generatedFailure.details}`
      : "";
    const guidanceSuffix =
      generatedFailure.status >= 500
        ? " oar-core may be unavailable; verify backend startup and base URL."
        : "";

    const requestError = new Error(
      `oar-core request failed at ${target}: ${method} ${path} (${generatedFailure.status ?? "unknown"})${detailSuffix}${guidanceSuffix}`,
    );
    requestError.status = generatedFailure.status;
    requestError.details = generatedFailure.details;
    return requestError;
  }

  const reason = error instanceof Error ? error.message : String(error);
  return new Error(
    `Unable to reach oar-core at ${target} for ${method} ${path}. Check that oar-core is running and OAR_CORE_BASE_URL is correct. ${reason}`,
  );
}

function buildRawRequestError({ status, details }, { target, method, path }) {
  const detailSuffix = details ? ` - ${details}` : "";
  const guidanceSuffix =
    status >= 500
      ? " oar-core may be unavailable; verify backend startup and base URL."
      : "";
  const requestError = new Error(
    `oar-core request failed at ${target}: ${method} ${path} (${status})${detailSuffix}${guidanceSuffix}`,
  );
  requestError.status = status;
  requestError.details = details;
  return requestError;
}

async function parseRawErrorResponse(response) {
  const rawDetails = await response.text().catch(() => "");
  const details = extractErrorMessage(rawDetails);
  return {
    status: response.status,
    details,
  };
}

export function createOarCoreClient(options = {}) {
  const resolvedBaseUrl = normalizeBaseUrl(options.baseUrl ?? "");
  const baseFetchFn = options.fetchFn ?? fetch;
  const actorIdProvider = options.actorIdProvider;
  const lockActorIdProvider = options.lockActorIdProvider;
  const tokenProvider = options.tokenProvider;
  const requestContextHeadersProvider = options.requestContextHeadersProvider;
  const target = resolvedBaseUrl || "same-origin";
  const sameOriginProxyBaseUrl = "http://oar.local";
  const generatedBaseUrl = resolvedBaseUrl || sameOriginProxyBaseUrl;

  const baseTransportFetch =
    resolvedBaseUrl.length > 0
      ? baseFetchFn
      : (input, init) => {
          const parsedUrl = new URL(String(input), sameOriginProxyBaseUrl);
          const relativeUrl = appPath(
            `${parsedUrl.pathname}${parsedUrl.search}`,
          );
          return baseFetchFn(relativeUrl, init);
        };

  function shouldLockActorId() {
    if (typeof lockActorIdProvider === "function") {
      return Boolean(lockActorIdProvider());
    }

    return Boolean(lockActorIdProvider);
  }

  function shouldSkipAuthRetry(input) {
    const parsedUrl = new URL(String(input), sameOriginProxyBaseUrl);
    return (
      parsedUrl.pathname === "/auth/token" ||
      parsedUrl.pathname === "/auth/agents/register" ||
      parsedUrl.pathname.startsWith("/auth/passkey/")
    );
  }

  const fetchFn = async (input, init = {}) => {
    async function performRequest({ retrying = false } = {}) {
      const headers = new Headers(init.headers ?? {});
      const requestContextHeaders =
        (await requestContextHeadersProvider?.()) ?? {};

      for (const [name, value] of Object.entries(requestContextHeaders)) {
        const normalizedValue = String(value ?? "").trim();
        if (!normalizedValue) {
          continue;
        }
        headers.set(name, normalizedValue);
      }

      if (retrying) {
        headers.delete("authorization");
      }

      if (!headers.has("authorization")) {
        const accessToken = await tokenProvider?.getAccessToken?.();
        if (accessToken) {
          headers.set("authorization", `Bearer ${accessToken}`);
        }
      }

      return baseTransportFetch(input, {
        ...init,
        headers,
      });
    }

    const response = await performRequest();
    if (
      response.status !== 401 ||
      !tokenProvider ||
      shouldSkipAuthRetry(input) ||
      !(await tokenProvider.hasRefreshToken?.())
    ) {
      return response;
    }

    try {
      const refreshedToken = await tokenProvider.refreshAccessToken?.();
      if (!refreshedToken) {
        await tokenProvider.handleRefreshFailure?.();
        return response;
      }
    } catch {
      await tokenProvider.handleRefreshFailure?.();
      return response;
    }

    return performRequest({ retrying: true });
  };

  const generated = new OarClient(generatedBaseUrl, fetchFn);

  function commandInfo(commandId) {
    const command = commandRegistryByID.get(commandId);
    if (!command) {
      throw new Error(`Unknown generated command id: ${commandId}`);
    }
    return command;
  }

  async function invokeJSON(commandId, invokeFn) {
    const command = commandInfo(commandId);

    try {
      const result = await invokeFn();
      return parseJsonBody(result.body, commandId);
    } catch (error) {
      throw normalizeRequestError(error, {
        target,
        commandId,
        method: command.method,
        path: command.path,
      });
    }
  }

  async function invokeRaw(commandId, pathParams = {}, options = {}) {
    const command = commandInfo(commandId);
    const resolvedPath = renderPath(command.path, pathParams);
    const queryString = buildQueryString(options.query);
    const requestPath = queryString
      ? `${resolvedPath}?${queryString}`
      : resolvedPath;
    const url = toAbsoluteUrl(resolvedBaseUrl, requestPath);

    let response;
    try {
      response = await fetchFn(url, {
        method: command.method,
        headers: {
          accept: options.accept ?? "*/*",
          ...(options.headers ?? {}),
        },
        signal: options.signal,
      });
    } catch (error) {
      throw normalizeRequestError(error, {
        target,
        commandId,
        method: command.method,
        path: command.path,
      });
    }

    if (!response.ok) {
      throw buildRawRequestError(await parseRawErrorResponse(response), {
        target,
        method: command.method,
        path: command.path,
      });
    }

    return response;
  }

  function requireActorId() {
    const actorId =
      typeof actorIdProvider === "function" ? actorIdProvider() : undefined;

    if (!actorId) {
      throw new Error(
        "No actor selected. Choose an actor before writing data.",
      );
    }

    return actorId;
  }

  function withActorId(payload = {}) {
    if (payload.actor_id && !shouldLockActorId()) {
      return payload;
    }

    return { ...payload, actor_id: requireActorId() };
  }

  return {
    baseUrl: resolvedBaseUrl,
    getVersion: () => invokeJSON("meta.version", () => generated.metaVersion()),
    getHandshake: () =>
      invokeJSON("meta.handshake", () => generated.metaHandshake()),

    createActor: (payload) =>
      invokeJSON("actors.register", () =>
        generated.actorsRegister({ body: payload }),
      ),
    listActors: (filters) =>
      invokeJSON("actors.list", () => generated.actorsList({ query: filters })),
    issueAuthToken: (payload) =>
      invokeJSON("auth.token", () => generated.authToken({ body: payload })),
    getCurrentAgent: () =>
      invokeJSON("agents.me.get", () => generated.agentsMeGet()),
    passkeyRegisterOptions: (payload) =>
      invokeJSON("auth.passkey.register.options", () =>
        generated.authPasskeyRegisterOptions({ body: payload }),
      ),
    passkeyRegisterVerify: (payload) =>
      invokeJSON("auth.passkey.register.verify", () =>
        generated.authPasskeyRegisterVerify({ body: payload }),
      ),
    passkeyLoginOptions: (payload) =>
      invokeJSON("auth.passkey.login.options", () =>
        generated.authPasskeyLoginOptions({ body: payload }),
      ),
    passkeyLoginVerify: (payload) =>
      invokeJSON("auth.passkey.login.verify", () =>
        generated.authPasskeyLoginVerify({ body: payload }),
      ),
    bootstrapStatus: () =>
      invokeJSON("auth.bootstrap.status", () =>
        generated.authBootstrapStatus(),
      ),
    listInvites: () =>
      invokeJSON("auth.invites.list", () => generated.authInvitesList()),
    createInvite: (payload) =>
      invokeJSON("auth.invites.create", () =>
        generated.authInvitesCreate({ body: payload }),
      ),
    revokeInvite: (inviteId) =>
      invokeJSON("auth.invites.revoke", () =>
        generated.authInvitesRevoke({ invite_id: String(inviteId) }),
      ),
    listPrincipals: (filters) =>
      invokeJSON("auth.principals.list", () =>
        generated.authPrincipalsList({ query: filters }),
      ),
    revokePrincipal: (agentId, payload = {}) =>
      invokeJSON("auth.principals.revoke", () =>
        generated.authPrincipalsRevoke(
          { agent_id: String(agentId) },
          { body: payload },
        ),
      ),
    listAuthAudit: (filters) =>
      invokeJSON("auth.audit.list", () =>
        generated.authAuditList({ query: filters }),
      ),

    createThread: (payload) =>
      invokeJSON("threads.create", () =>
        generated.threadsCreate({ body: withActorId(payload) }),
      ),
    listThreads: (filters) =>
      invokeJSON("threads.list", () =>
        generated.threadsList({ query: filters }),
      ),
    archiveThread: (threadId, payload) =>
      invokeJSON("threads.archive", () =>
        generated.threadsArchive(
          { thread_id: String(threadId) },
          { body: withActorId(payload) },
        ),
      ),
    unarchiveThread: (threadId, payload) =>
      invokeJSON("threads.unarchive", () =>
        generated.threadsUnarchive(
          { thread_id: String(threadId) },
          { body: withActorId(payload) },
        ),
      ),
    tombstoneThread: (threadId, payload) =>
      invokeJSON("threads.tombstone", () =>
        generated.threadsTombstone(
          { thread_id: String(threadId) },
          { body: withActorId(payload) },
        ),
      ),
    restoreThread: (threadId, payload) =>
      invokeJSON("threads.restore", () =>
        generated.threadsRestore(
          { thread_id: String(threadId) },
          { body: withActorId(payload) },
        ),
      ),
    purgeThread: (threadId, payload) =>
      invokeJSON("threads.purge", () =>
        generated.threadsPurge(
          { thread_id: String(threadId) },
          { body: payload || {} },
        ),
      ),
    getThread: (threadId) =>
      invokeJSON("threads.get", () =>
        generated.threadsGet({ thread_id: String(threadId) }),
      ),
    updateThread: (threadId, payload) =>
      invokeJSON("threads.patch", () =>
        generated.threadsPatch(
          { thread_id: String(threadId) },
          { body: withActorId(payload) },
        ),
      ),
    getThreadWorkspace: (threadId, filters) =>
      invokeJSON("threads.workspace", () =>
        generated.threadsWorkspace(
          { thread_id: String(threadId) },
          { query: filters },
        ),
      ),
    listThreadTimeline: (threadId) =>
      invokeJSON("threads.timeline", () =>
        generated.threadsTimeline({ thread_id: String(threadId) }),
      ),
    streamThreadEvents: async ({ threadId, lastEventId, signal, onEvent }) => {
      const response = await invokeRaw(
        "events.stream",
        {},
        {
          query: {
            thread_id: String(threadId),
            last_event_id: lastEventId,
          },
          accept: "text/event-stream",
          signal,
        },
      );
      await consumeSSEStream(response, { onEvent, signal });
    },
    getSnapshot: (snapshotId) =>
      invokeJSON("snapshots.get", () =>
        generated.snapshotsGet({ snapshot_id: String(snapshotId) }),
      ),

    createCommitment: (payload) =>
      invokeJSON("commitments.create", () =>
        generated.commitmentsCreate({ body: withActorId(payload) }),
      ),
    listCommitments: (filters) =>
      invokeJSON("commitments.list", () =>
        generated.commitmentsList({ query: filters }),
      ),
    getCommitment: (commitmentId) =>
      invokeJSON("commitments.get", () =>
        generated.commitmentsGet({ commitment_id: String(commitmentId) }),
      ),
    updateCommitment: (commitmentId, payload) =>
      invokeJSON("commitments.patch", () =>
        generated.commitmentsPatch(
          { commitment_id: String(commitmentId) },
          { body: withActorId(payload) },
        ),
      ),

    createArtifact: (payload) =>
      invokeJSON("artifacts.create", () =>
        generated.artifactsCreate({ body: withActorId(payload) }),
      ),
    listArtifacts: (filters) =>
      invokeJSON("artifacts.list", () =>
        generated.artifactsList({ query: filters }),
      ),
    getArtifact: (artifactId) =>
      invokeJSON("artifacts.get", () =>
        generated.artifactsGet({ artifact_id: String(artifactId) }),
      ),
    archiveArtifact: (artifactId, payload) =>
      invokeJSON("artifacts.archive", () =>
        generated.artifactsArchive(
          { artifact_id: String(artifactId) },
          { body: withActorId(payload) },
        ),
      ),
    unarchiveArtifact: (artifactId, payload) =>
      invokeJSON("artifacts.unarchive", () =>
        generated.artifactsUnarchive(
          { artifact_id: String(artifactId) },
          { body: withActorId(payload) },
        ),
      ),
    tombstoneArtifact: (artifactId, payload) =>
      invokeJSON("artifacts.tombstone", () =>
        generated.artifactsTombstone(
          { artifact_id: String(artifactId) },
          { body: withActorId(payload) },
        ),
      ),
    restoreArtifact: (artifactId, payload) =>
      invokeJSON("artifacts.restore", () =>
        generated.artifactsRestore(
          { artifact_id: String(artifactId) },
          { body: withActorId(payload) },
        ),
      ),
    purgeArtifact: (artifactId, payload) =>
      invokeJSON("artifacts.purge", () =>
        generated.artifactsPurge(
          { artifact_id: String(artifactId) },
          { body: payload || {} },
        ),
      ),
    getArtifactContent: async (artifactId) => {
      const response = await invokeRaw("artifacts.content.get", {
        artifact_id: String(artifactId),
      });

      const contentType = response.headers.get("content-type") ?? "";

      if (contentType.includes("application/json")) {
        return { contentType, content: await response.json() };
      }

      if (contentType.startsWith("text/")) {
        return { contentType, content: await response.text() };
      }

      return { contentType, content: await response.arrayBuffer() };
    },

    createDocument: (payload) =>
      invokeJSON("docs.create", () =>
        generated.docsCreate({ body: withActorId(payload) }),
      ),
    listDocuments: (filters) =>
      invokeJSON("docs.list", () => generated.docsList({ query: filters })),
    getDocument: (documentId) =>
      invokeJSON("docs.get", () =>
        generated.docsGet({ document_id: String(documentId) }),
      ),
    getDocumentHistory: (documentId) =>
      invokeJSON("docs.history", () =>
        generated.docsHistory({ document_id: String(documentId) }),
      ),
    getDocumentRevision: (documentId, revisionId) =>
      invokeJSON("docs.revision.get", () =>
        generated.docsRevisionGet({
          document_id: String(documentId),
          revision_id: String(revisionId),
        }),
      ),
    updateDocument: (documentId, payload) =>
      invokeJSON("docs.update", () =>
        generated.docsUpdate(
          { document_id: String(documentId) },
          { body: withActorId(payload) },
        ),
      ),
    tombstoneDocument: (documentId, payload) =>
      invokeJSON("docs.tombstone", () =>
        generated.docsTombstone(
          { document_id: String(documentId) },
          { body: withActorId(payload) },
        ),
      ),
    archiveDocument: (documentId, payload) =>
      invokeJSON("docs.archive", () =>
        generated.docsArchive(
          { document_id: String(documentId) },
          { body: withActorId(payload) },
        ),
      ),
    unarchiveDocument: (documentId, payload) =>
      invokeJSON("docs.unarchive", () =>
        generated.docsUnarchive(
          { document_id: String(documentId) },
          { body: withActorId(payload) },
        ),
      ),
    restoreDocument: (documentId, payload) =>
      invokeJSON("docs.restore", () =>
        generated.docsRestore(
          { document_id: String(documentId) },
          { body: withActorId(payload) },
        ),
      ),
    purgeDocument: (documentId, payload) =>
      invokeJSON("docs.purge", () =>
        generated.docsPurge(
          { document_id: String(documentId) },
          { body: payload || {} },
        ),
      ),

    createEvent: (payload) =>
      invokeJSON("events.create", () =>
        generated.eventsCreate({ body: withActorId(payload) }),
      ),
    getEvent: (eventId) =>
      invokeJSON("events.get", () =>
        generated.eventsGet({ event_id: String(eventId) }),
      ),
    archiveEvent: (eventId, payload) =>
      invokeJSON("events.archive", () =>
        generated.eventsArchive(
          { event_id: String(eventId) },
          { body: withActorId(payload) },
        ),
      ),
    unarchiveEvent: (eventId, payload) =>
      invokeJSON("events.unarchive", () =>
        generated.eventsUnarchive(
          { event_id: String(eventId) },
          { body: withActorId(payload) },
        ),
      ),
    tombstoneEvent: (eventId, payload) =>
      invokeJSON("events.tombstone", () =>
        generated.eventsTombstone(
          { event_id: String(eventId) },
          { body: withActorId(payload) },
        ),
      ),
    restoreEvent: (eventId, payload) =>
      invokeJSON("events.restore", () =>
        generated.eventsRestore(
          { event_id: String(eventId) },
          { body: withActorId(payload) },
        ),
      ),

    createWorkOrder: (payload) =>
      invokeJSON("packets.work-orders.create", () =>
        generated.packetsWorkOrdersCreate({ body: withActorId(payload) }),
      ),
    createReceipt: (payload) =>
      invokeJSON("packets.receipts.create", () =>
        generated.packetsReceiptsCreate({ body: withActorId(payload) }),
      ),
    createReview: (payload) =>
      invokeJSON("packets.reviews.create", () =>
        generated.packetsReviewsCreate({ body: withActorId(payload) }),
      ),

    listInboxItems: (filters) =>
      invokeJSON("inbox.list", () => generated.inboxList({ query: filters })),
    ackInboxItem: (payload) =>
      invokeJSON("inbox.ack", () =>
        generated.inboxAck({ body: withActorId(payload) }),
      ),

    createBoard: (payload) =>
      invokeJSON("boards.create", () =>
        generated.boardsCreate({ body: withActorId(payload) }),
      ),
    listBoards: (filters) =>
      invokeJSON("boards.list", () => generated.boardsList({ query: filters })),
    getBoard: (boardId) =>
      invokeJSON("boards.get", () =>
        generated.boardsGet({ board_id: String(boardId) }),
      ),
    updateBoard: (boardId, payload) =>
      invokeJSON("boards.update", () =>
        generated.boardsUpdate(
          { board_id: String(boardId) },
          { body: withActorId(payload) },
        ),
      ),
    getBoardWorkspace: (boardId) =>
      invokeJSON("boards.workspace", () =>
        generated.boardsWorkspace({ board_id: String(boardId) }),
      ),
    archiveBoard: (boardId, payload) =>
      invokeJSON("boards.archive", () =>
        generated.boardsArchive(
          { board_id: String(boardId) },
          { body: withActorId(payload) },
        ),
      ),
    unarchiveBoard: (boardId, payload) =>
      invokeJSON("boards.unarchive", () =>
        generated.boardsUnarchive(
          { board_id: String(boardId) },
          { body: withActorId(payload) },
        ),
      ),
    tombstoneBoard: (boardId, payload) =>
      invokeJSON("boards.tombstone", () =>
        generated.boardsTombstone(
          { board_id: String(boardId) },
          { body: withActorId(payload) },
        ),
      ),
    restoreBoard: (boardId, payload) =>
      invokeJSON("boards.restore", () =>
        generated.boardsRestore(
          { board_id: String(boardId) },
          { body: withActorId(payload) },
        ),
      ),
    purgeBoard: (boardId, payload) =>
      invokeJSON("boards.purge", () =>
        generated.boardsPurge(
          { board_id: String(boardId) },
          { body: payload || {} },
        ),
      ),

    addBoardCard: (boardId, payload) =>
      invokeJSON("boards.cards.add", () =>
        generated.boardsCardsAdd(
          { board_id: String(boardId) },
          { body: withActorId(payload) },
        ),
      ),
    listBoardCards: (boardId) =>
      invokeJSON("boards.cards.list", () =>
        generated.boardsCardsList({ board_id: String(boardId) }),
      ),
    moveBoardCard: (boardId, cardId, payload) =>
      invokeJSON("boards.cards.move", () =>
        generated.boardsCardsMove(
          { board_id: String(boardId), thread_id: String(cardId) },
          { body: withActorId(payload) },
        ),
      ),
    removeBoardCard: (boardId, cardId, payload) =>
      invokeJSON("boards.cards.remove", () =>
        generated.boardsCardsRemove(
          { board_id: String(boardId), thread_id: String(cardId) },
          { body: withActorId(payload) },
        ),
      ),
    updateBoardCard: (boardId, cardId, payload) =>
      invokeJSON("boards.cards.update", () =>
        generated.boardsCardsUpdate(
          { board_id: String(boardId), thread_id: String(cardId) },
          { body: withActorId(payload) },
        ),
      ),
  };
}

export async function verifyCoreSchemaVersion(
  client,
  expectedSchemaVersion = EXPECTED_SCHEMA_VERSION,
) {
  const target = client.baseUrl || "same-origin";
  const expectedCommandRegistryDigest =
    await getExpectedCommandRegistryDigest();

  let version;
  try {
    version = await getHandshakeOrVersion(client);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    throw new Error(
      `Unable to verify oar-core schema version at ${target}: ${reason}`,
    );
  }

  if (
    !version ||
    (typeof version === "object" && Object.keys(version).length === 0)
  ) {
    throw new Error(
      `oar-core handshake at ${target} returned an empty response. ` +
        "The UI server may not be running server-side code " +
        "(e.g. vite preview does not execute SvelteKit hooks). " +
        "Use the Node adapter build (ADAPTER=node) and serve with " +
        "'node build/index.js' or './scripts/serve'.",
    );
  }

  if (version?.schema_version !== expectedSchemaVersion) {
    throw new Error(
      `oar-core schema mismatch at ${target}: expected ${expectedSchemaVersion}, received ${version?.schema_version ?? "unknown"}.`,
    );
  }

  if (version?.command_registry_digest !== expectedCommandRegistryDigest) {
    throw new Error(
      `oar-core contract mismatch at ${target}: expected command registry digest ${expectedCommandRegistryDigest}, received ${version?.command_registry_digest ?? "missing"}. This usually means the web UI is newer than the deployed core and may call endpoints that core does not implement yet.`,
    );
  }

  return version;
}

async function getHandshakeOrVersion(client) {
  try {
    return await client.getHandshake();
  } catch (error) {
    if (error?.status !== 404) {
      throw error;
    }
  }

  return client.getVersion();
}
