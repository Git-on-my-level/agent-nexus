import {
  AnxClient,
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
    throw new Error(`anx-core returned invalid JSON for ${commandId}.`);
  }
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
    throw new Error("anx-core returned an empty event stream response body.");
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
        ? " anx-core may be unavailable; verify backend startup and base URL."
        : "";

    const requestError = new Error(
      `anx-core request failed at ${target}: ${method} ${path} (${generatedFailure.status ?? "unknown"})${detailSuffix}${guidanceSuffix}`,
    );
    requestError.status = generatedFailure.status;
    requestError.details = generatedFailure.details;
    return requestError;
  }

  const reason = error instanceof Error ? error.message : String(error);
  return new Error(
    `Unable to reach anx-core at ${target} for ${method} ${path}. Check that anx-core is running and ANX_CORE_BASE_URL is correct. ${reason}`,
  );
}

function buildRawRequestError({ status, details }, { target, method, path }) {
  const detailSuffix = details ? ` - ${details}` : "";
  const guidanceSuffix =
    status >= 500
      ? " anx-core may be unavailable; verify backend startup and base URL."
      : "";
  const requestError = new Error(
    `anx-core request failed at ${target}: ${method} ${path} (${status})${detailSuffix}${guidanceSuffix}`,
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

function pathParams(entries) {
  return Object.fromEntries(
    Object.entries(entries).map(([name, value]) => [name, String(value)]),
  );
}

const q = (query) => ({ query });
const b = (body) => ({ body });
const pq = (pathParams, query) => ({ pathParams, options: q(query) });
const pb = (pathParams, body) => ({
  pathParams,
  options: b(body),
});
const p = (pathParams) => ({ pathParams });

const adapterCommandTable = [
  ["getVersion", "meta.version"],
  ["getHandshake", "meta.handshake"],
  ["createActor", "actors.create", (payload) => ({ options: b(payload) })],
  ["listActors", "actors.list", (filters) => ({ options: q(filters) })],
  ["issueAuthToken", "auth.token", (payload) => ({ options: b(payload) })],
  ["getCurrentAgent", "agents.me.get"],
  [
    "passkeyRegisterOptions",
    "auth.passkey.register.options",
    (payload) => ({ options: b(payload) }),
  ],
  [
    "passkeyRegisterVerify",
    "auth.passkey.register.verify",
    (payload) => ({ options: b(payload) }),
  ],
  [
    "passkeyLoginOptions",
    "auth.passkey.login.options",
    (payload) => ({ options: b(payload) }),
  ],
  [
    "passkeyLoginVerify",
    "auth.passkey.login.verify",
    (payload) => ({ options: b(payload) }),
  ],
  [
    "passkeyDevRegister",
    "auth.passkey.dev.register",
    (payload) => ({ options: b(payload) }),
  ],
  [
    "passkeyDevLogin",
    "auth.passkey.dev.login",
    (payload) => ({ options: b(payload) }),
  ],
  ["bootstrapStatus", "auth.bootstrap.status"],
  ["listInvites", "auth.invites.list"],
  [
    "createInvite",
    "auth.invites.create",
    (payload) => ({ options: b(payload) }),
  ],
  [
    "revokeInvite",
    "auth.invites.revoke",
    (inviteId) => pb(pathParams({ invite_id: inviteId }), {}),
  ],
  [
    "listPrincipals",
    "auth.principals.list",
    (filters) => ({ options: q(filters) }),
  ],
  [
    "revokePrincipal",
    "auth.principals.revoke",
    (principalId, payload = {}) =>
      pb(pathParams({ principal_id: principalId }), payload),
  ],
  ["listAuthAudit", "auth.audit.list", (filters) => ({ options: q(filters) })],
  ["listSecrets", "secrets.list"],
  ["createSecret", "secrets.create", (payload) => ({ options: b(payload) })],
  [
    "updateSecret",
    "secrets.update",
    (secretId, payload) => pb(pathParams({ secret_id: secretId }), payload),
  ],
  [
    "deleteSecret",
    "secrets.delete",
    (secretId) => p(pathParams({ secret_id: secretId })),
  ],
  [
    "revealSecret",
    "secrets.reveal",
    (secretId) => p(pathParams({ secret_id: secretId })),
  ],
  ["listThreads", "threads.list", (filters) => ({ options: q(filters) })],
  [
    "getThread",
    "threads.inspect",
    (threadId) => p(pathParams({ thread_id: threadId })),
  ],
  [
    "getThreadWorkspace",
    "threads.workspace",
    (threadId, filters) => pq(pathParams({ thread_id: threadId }), filters),
  ],
  [
    "listThreadTimeline",
    "threads.timeline",
    (threadId, opts) => ({
      pathParams: pathParams({ thread_id: threadId }),
      options: opts ?? {},
    }),
  ],
  [
    "getTopicWorkspace",
    "topics.workspace",
    (topicId, filters) => pq(pathParams({ topic_id: topicId }), filters),
  ],
  [
    "listTopicTimeline",
    "topics.timeline",
    (topicId, opts) => ({
      pathParams: pathParams({ topic_id: topicId }),
      options: opts ?? {},
    }),
  ],
  ["listTopics", "topics.list", (filters) => ({ options: q(filters) })],
  [
    "createTopic",
    "topics.create",
    (payload) => ({ options: b(payload), injectActor: true }),
  ],
  ["getTopic", "topics.get", (topicId) => p(pathParams({ topic_id: topicId }))],
  [
    "updateTopic",
    "topics.patch",
    (topicId, payload) => pb(pathParams({ topic_id: topicId }), payload),
    true,
  ],
  [
    "archiveTopic",
    "topics.archive",
    (topicId, payload) => pb(pathParams({ topic_id: topicId }), payload, true),
    true,
  ],
  [
    "unarchiveTopic",
    "topics.unarchive",
    (topicId, payload) => pb(pathParams({ topic_id: topicId }), payload, true),
    true,
  ],
  [
    "trashTopic",
    "topics.trash",
    (topicId, payload) => pb(pathParams({ topic_id: topicId }), payload, true),
    true,
  ],
  [
    "restoreTopic",
    "topics.restore",
    (topicId, payload) => pb(pathParams({ topic_id: topicId }), payload, true),
    true,
  ],
  ["listCards", "cards.list", (filters) => ({ options: q(filters) })],
  ["getCard", "cards.get", (cardId) => p(pathParams({ card_id: cardId }))],
  [
    "archiveCard",
    "cards.archive",
    (cardId, payload) => pb(pathParams({ card_id: cardId }), payload, true),
    true,
  ],
  [
    "restoreCard",
    "cards.restore",
    (cardId, payload) => pb(pathParams({ card_id: cardId }), payload, true),
    true,
  ],
  [
    "listCardTimeline",
    "cards.timeline",
    (cardId, opts) => ({
      pathParams: pathParams({ card_id: cardId }),
      options: opts ?? {},
    }),
  ],
  [
    "purgeCard",
    "cards.purge",
    (cardId, payload) => pb(pathParams({ card_id: cardId }), payload, true),
    true,
  ],
  [
    "createArtifact",
    "artifacts.create",
    (payload) => ({ options: b(payload), injectActor: true }),
  ],
  ["listArtifacts", "artifacts.list", (filters) => ({ options: q(filters) })],
  [
    "getArtifact",
    "artifacts.get",
    (artifactId) => p(pathParams({ artifact_id: artifactId })),
  ],
  [
    "archiveArtifact",
    "artifacts.archive",
    (artifactId, payload) =>
      pb(pathParams({ artifact_id: artifactId }), payload),
    true,
  ],
  [
    "unarchiveArtifact",
    "artifacts.unarchive",
    (artifactId, payload) =>
      pb(pathParams({ artifact_id: artifactId }), payload),
    true,
  ],
  [
    "trashArtifact",
    "artifacts.trash",
    (artifactId, payload) =>
      pb(pathParams({ artifact_id: artifactId }), payload),
    true,
  ],
  [
    "restoreArtifact",
    "artifacts.restore",
    (artifactId, payload) =>
      pb(pathParams({ artifact_id: artifactId }), payload),
    true,
  ],
  [
    "purgeArtifact",
    "artifacts.purge",
    (artifactId, payload) =>
      pb(pathParams({ artifact_id: artifactId }), payload),
    true,
  ],
  [
    "createDocument",
    "docs.create",
    (payload) => ({ options: b(payload), injectActor: true }),
  ],
  ["listDocuments", "docs.list", (filters) => ({ options: q(filters) })],
  [
    "getDocument",
    "docs.get",
    (documentId) => p(pathParams({ document_id: documentId })),
  ],
  [
    "patchDocument",
    "docs.patch",
    (documentId, payload) =>
      pb(pathParams({ document_id: documentId }), payload),
    true,
  ],
  [
    "getDocumentHistory",
    "docs.revisions.list",
    (documentId) => p(pathParams({ document_id: documentId })),
  ],
  [
    "getDocumentRevision",
    "docs.revisions.get",
    (documentId, revisionId) =>
      p(pathParams({ document_id: documentId, revision_id: revisionId })),
  ],
  [
    "updateDocument",
    "docs.revisions.create",
    (documentId, payload) =>
      pb(pathParams({ document_id: documentId }), payload),
    true,
  ],
  [
    "getCardHistory",
    "cards.revisions.list",
    (cardId) => p(pathParams({ card_id: cardId })),
  ],
  [
    "getCardRevision",
    "cards.revisions.get",
    (cardId, revisionId) =>
      p(pathParams({ card_id: cardId, revision_id: revisionId })),
  ],
  [
    "updateCardContent",
    "cards.revisions.create",
    (cardId, payload) => pb(pathParams({ card_id: cardId }), payload),
    true,
  ],
  [
    "trashDocument",
    "docs.trash",
    (documentId, payload) =>
      pb(pathParams({ document_id: documentId }), payload),
    true,
  ],
  [
    "archiveDocument",
    "docs.archive",
    (documentId, payload) =>
      pb(pathParams({ document_id: documentId }), payload),
    true,
  ],
  [
    "unarchiveDocument",
    "docs.unarchive",
    (documentId, payload) =>
      pb(pathParams({ document_id: documentId }), payload),
    true,
  ],
  [
    "restoreDocument",
    "docs.restore",
    (documentId, payload) =>
      pb(pathParams({ document_id: documentId }), payload),
    true,
  ],
  [
    "purgeDocument",
    "docs.purge",
    (documentId, payload) =>
      pb(pathParams({ document_id: documentId }), payload),
    true,
  ],
  [
    "createEvent",
    "events.create",
    (payload) => ({ options: b(payload), injectActor: true }),
  ],
  ["listEvents", "events.list", (filters) => ({ options: q(filters) })],
  ["getEvent", "events.get", (eventId) => p(pathParams({ event_id: eventId }))],
  [
    "archiveEvent",
    "events.archive",
    (eventId, payload) => pb(pathParams({ event_id: eventId }), payload),
    true,
  ],
  [
    "unarchiveEvent",
    "events.unarchive",
    (eventId, payload) => pb(pathParams({ event_id: eventId }), payload),
    true,
  ],
  [
    "trashEvent",
    "events.trash",
    (eventId, payload) => pb(pathParams({ event_id: eventId }), payload),
    true,
  ],
  [
    "restoreEvent",
    "events.restore",
    (eventId, payload) => pb(pathParams({ event_id: eventId }), payload),
    true,
  ],
  ["listInboxItems", "inbox.list", (filters) => ({ options: q(filters) })],
  [
    "createBoard",
    "boards.create",
    (payload) => ({ options: b(payload), injectActor: true }),
  ],
  ["listBoards", "boards.list", (filters) => ({ options: q(filters) })],
  ["getBoard", "boards.get", (boardId) => p(pathParams({ board_id: boardId }))],
  [
    "updateBoard",
    "boards.patch",
    (boardId, payload) => pb(pathParams({ board_id: boardId }), payload),
    true,
  ],
  [
    "getBoardWorkspace",
    "boards.workspace",
    (boardId) => p(pathParams({ board_id: boardId })),
  ],
  [
    "archiveBoard",
    "boards.archive",
    (boardId, payload) => pb(pathParams({ board_id: boardId }), payload),
    true,
  ],
  [
    "unarchiveBoard",
    "boards.unarchive",
    (boardId, payload) => pb(pathParams({ board_id: boardId }), payload),
    true,
  ],
  [
    "trashBoard",
    "boards.trash",
    (boardId, payload) => pb(pathParams({ board_id: boardId }), payload),
    true,
  ],
  [
    "restoreBoard",
    "boards.restore",
    (boardId, payload) => pb(pathParams({ board_id: boardId }), payload),
    true,
  ],
  [
    "purgeBoard",
    "boards.purge",
    (boardId, payload) => pb(pathParams({ board_id: boardId }), payload),
    true,
  ],
  [
    "addBoardCard",
    "cards.create",
    (boardId, payload = {}) => {
      const { if_board_updated_at, request_key, ...card } = payload;
      return {
        options: b({
          board_id: boardId,
          ...(if_board_updated_at != null && if_board_updated_at !== ""
            ? { if_board_updated_at }
            : {}),
          ...(request_key ? { request_key } : {}),
          card,
        }),
      };
    },
    true,
  ],
  [
    "listBoardCards",
    "boards.cards.list",
    (boardId) => p(pathParams({ board_id: boardId })),
  ],
  [
    "moveBoardCard",
    "cards.move",
    (_boardId, cardId, payload) => pb(pathParams({ card_id: cardId }), payload),
    true,
  ],
  [
    "removeBoardCard",
    "cards.archive",
    (_boardId, cardId, payload) =>
      pb(pathParams({ card_id: cardId }), payload, true),
    true,
  ],
  [
    "updateBoardCard",
    "cards.patch",
    (_boardId, cardId, payload) => pb(pathParams({ card_id: cardId }), payload),
    true,
  ],
];

export function createAnxCoreClient(options = {}) {
  const resolvedBaseUrl = normalizeBaseUrl(options.baseUrl ?? "");
  const baseFetchFn = options.fetchFn ?? fetch;
  const actorIdProvider = options.actorIdProvider;
  const lockActorIdProvider = options.lockActorIdProvider;
  const tokenProvider = options.tokenProvider;
  const requestContextHeadersProvider = options.requestContextHeadersProvider;
  const target = resolvedBaseUrl || "same-origin";
  const sameOriginProxyBaseUrl = "http://anx.local";
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

  const generated = new AnxClient(generatedBaseUrl, fetchFn);

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
      const msg = error instanceof Error ? error.message : String(error);
      if (msg.startsWith("No actor selected.")) {
        throw error;
      }
      throw normalizeRequestError(error, {
        target,
        commandId,
        method: command.method,
        path: command.path,
      });
    }
  }

  async function invokeDirectRaw(
    path,
    {
      method = "GET",
      query = {},
      headers = {},
      accept = "*/*",
      signal,
      body,
    } = {},
  ) {
    const queryString = buildQueryString(query);
    const requestPath = queryString ? `${path}?${queryString}` : path;
    const url = toAbsoluteUrl(resolvedBaseUrl, requestPath);

    let response;
    try {
      response = await fetchFn(url, {
        method,
        headers: {
          accept,
          ...headers,
        },
        ...(body !== undefined ? { body } : {}),
        signal,
      });
    } catch (error) {
      throw normalizeRequestError(error, {
        target,
        commandId: `direct:${method} ${path}`,
        method,
        path,
      });
    }

    if (!response.ok) {
      throw buildRawRequestError(await parseRawErrorResponse(response), {
        target,
        method,
        path,
      });
    }

    return response;
  }

  function requireActorId() {
    const actorId =
      typeof actorIdProvider === "function" ? actorIdProvider() : undefined;

    if (!actorId) {
      // Authenticated sessions lock identity to the principal; core
      // resolveWriteActorID uses JWT principal.ActorID when body actor_id is empty.
      if (shouldLockActorId()) {
        return "";
      }
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

  function readerQuery() {
    if (shouldLockActorId()) {
      return {};
    }
    return { reader_id: requireActorId() };
  }

  function callGeneratedCommand(command, pathParams = {}, requestOptions = {}) {
    const generatedMethod = generated[command.ts_method];
    if (typeof generatedMethod !== "function") {
      throw new Error(
        `Generated client is missing method ${command.ts_method} for ${command.command_id}.`,
      );
    }

    if (command.path_params?.length > 0) {
      return generatedMethod.call(generated, pathParams, requestOptions);
    }

    return generatedMethod.call(generated, requestOptions);
  }

  async function invokeCommand(commandId, request = {}) {
    const command = commandInfo(commandId);
    const requestOptions = { ...(request.options ?? {}) };

    return invokeJSON(commandId, () => {
      if (request.injectActor && command.method !== "GET") {
        requestOptions.body = withActorId(requestOptions.body ?? {});
      }
      return callGeneratedCommand(
        command,
        request.pathParams ?? {},
        requestOptions,
      );
    });
  }

  const tableDrivenClient = Object.fromEntries(
    adapterCommandTable.map(([name, commandId, shapeArgs, injectActor]) => [
      name,
      (...args) => {
        const request =
          typeof shapeArgs === "function" ? shapeArgs(...args) : {};
        return invokeCommand(commandId, {
          ...request,
          injectActor: Boolean(injectActor || request.injectActor),
        });
      },
    ]),
  );

  function normalizeHomeReadPayload(payload) {
    const body = { ...payload };
    if (body.topic_id) {
      body.group_ref = body.group_ref ?? `topic:${body.topic_id}`;
      delete body.topic_id;
    }
    if (body.topic_ids) {
      body.group_refs =
        body.group_refs ?? body.topic_ids.map((id) => `topic:${id}`);
      delete body.topic_ids;
    }
    if (body.topic_cursors) {
      body.group_cursors =
        body.group_cursors ??
        Object.fromEntries(
          Object.entries(body.topic_cursors).map(([id, cursor]) => [
            `topic:${id}`,
            cursor,
          ]),
        );
      delete body.topic_cursors;
    }
    return body;
  }

  return {
    baseUrl: resolvedBaseUrl,
    ...tableDrivenClient,
    streamThreadEvents: async ({ threadId, lastEventId, signal, onEvent }) => {
      const response = await invokeDirectRaw("/stream/events", {
        query: {
          thread_id: String(threadId),
          last_event_id: lastEventId,
        },
        accept: "text/event-stream",
        signal,
      });
      await consumeSSEStream(response, { onEvent, signal });
    },
    streamNotificationReceipts: async ({
      threadId,
      lastEventId,
      signal,
      onReceipt,
    }) => {
      const response = await invokeDirectRaw(
        "/stream/agent-notification-receipts",
        {
          query: {
            thread_id: String(threadId),
            last_event_id: lastEventId,
          },
          accept: "text/event-stream",
          signal,
        },
      );
      await consumeSSEStream(response, {
        signal,
        onEvent: async (message) => {
          if (message?.event !== "notification_receipt") {
            return;
          }
          const receipt =
            message?.data &&
            typeof message.data === "object" &&
            !Array.isArray(message.data)
              ? message.data.receipt
              : null;
          if (receipt && typeof receipt === "object") {
            await onReceipt?.(receipt, message);
          }
        },
      });
    },
    /**
     * Multipart attachment upload. `file` should be a browser File or Blob (optional name on File).
     * @param {{ refs: string[], file: Blob, summary?: string, artifact?: Record<string, unknown> }} opts
     */
    createArtifactAttachment: async (opts) => {
      const refs = opts?.refs;
      if (!Array.isArray(refs) || refs.length === 0) {
        throw new Error("createArtifactAttachment: refs array is required");
      }
      const file = opts?.file;
      if (!(file instanceof Blob)) {
        throw new Error("createArtifactAttachment: file Blob is required");
      }
      const form = new FormData();
      form.append("refs", JSON.stringify(refs));
      const fname =
        file instanceof File && file.name ? file.name : "attachment.bin";
      form.append("file", file, fname);
      if (opts.summary) form.append("summary", String(opts.summary));
      if (opts.artifact && typeof opts.artifact === "object") {
        form.append("artifact", JSON.stringify(opts.artifact));
      }
      const aid = requireActorId();
      if (aid) form.append("actor_id", aid);

      const response = await invokeDirectRaw("/artifacts/attachments", {
        method: "POST",
        body: form,
        accept: "application/json",
      });
      return parseJsonBody(
        await response.text(),
        "artifacts.attachments.create",
      );
    },
    getArtifactContent: async (artifactId) => {
      const response = await invokeDirectRaw(
        `/artifacts/${encodeURIComponent(String(artifactId))}/content`,
        { method: "GET" },
      );

      const contentType = response.headers.get("content-type") ?? "";

      if (contentType.includes("application/json")) {
        return { contentType, content: await response.json() };
      }

      if (contentType.startsWith("text/")) {
        return { contentType, content: await response.text() };
      }

      return { contentType, content: await response.arrayBuffer() };
    },
    getHomeUnread: () =>
      invokeCommand("home.unread", { options: { query: readerQuery() } }),
    markHomeRead: (payload) =>
      invokeCommand("home.read", {
        options: { body: normalizeHomeReadPayload(payload) },
        injectActor: true,
      }),
    getInboxItem: (inboxItemId, filters) => {
      if (!inboxItemId) {
        throw new Error("getInboxItem requires inboxItemId.");
      }
      return invokeCommand("inbox.get", {
        pathParams: pathParams({ inbox_id: inboxItemId }),
        options: { query: filters ?? {} },
      });
    },
    respondInboxItem: async (inboxItemId, payload) => {
      const id = String(inboxItemId ?? "").trim();
      if (!id) {
        throw new Error("respondInboxItem requires inboxItemId.");
      }
      const responseText = String(payload?.response_text ?? "").trim();
      if (!responseText) {
        throw new Error("respondInboxItem requires a non-empty response_text.");
      }
      return invokeCommand("inbox.respond", {
        pathParams: pathParams({ inbox_id: id }),
        options: {
          body: withActorId({
            ...payload,
            response_text: responseText,
          }),
        },
      });
    },
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
    const wrapped = new Error(
      `Unable to verify anx-core schema version at ${target}: ${reason}`,
    );
    if (error instanceof Error) {
      wrapped.cause = error;
      if (typeof error.status === "number") {
        wrapped.coreHttpStatus = error.status;
      }
    }
    throw wrapped;
  }

  if (
    !version ||
    (typeof version === "object" && Object.keys(version).length === 0)
  ) {
    throw new Error(
      `anx-core handshake at ${target} returned an empty response. ` +
        "The UI server may not be running server-side code " +
        "(e.g. vite preview does not execute SvelteKit hooks). " +
        "Use the Node adapter build (ADAPTER=node) and serve with " +
        "'node build/index.js' or './scripts/serve'.",
    );
  }

  if (version?.schema_version !== expectedSchemaVersion) {
    throw new Error(
      `anx-core schema mismatch at ${target}: expected ${expectedSchemaVersion}, received ${version?.schema_version ?? "unknown"}.`,
    );
  }

  if (version?.command_registry_digest !== expectedCommandRegistryDigest) {
    throw new Error(
      `anx-core contract mismatch at ${target}: expected command registry digest ${expectedCommandRegistryDigest}, received ${version?.command_registry_digest ?? "missing"}. This usually means the web UI is newer than the deployed core and may call endpoints that core does not implement yet.`,
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
