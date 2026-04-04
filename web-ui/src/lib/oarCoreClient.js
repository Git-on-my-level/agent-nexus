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

/** 0.3.x packets require `subject_ref`; legacy UI still sends `thread_id` on the packet. */
function normalizePacketThreadIdToSubjectRef(packet) {
  if (!packet || typeof packet !== "object") {
    return packet;
  }
  const next = { ...packet };
  const threadId = String(next.thread_id ?? "").trim();
  delete next.thread_id;
  if (threadId && !String(next.subject_ref ?? "").trim()) {
    next.subject_ref = `thread:${threadId}`;
  }
  return next;
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

  async function invokeDirectJSON(path, options = {}) {
    const method = String(options.method ?? "GET").toUpperCase();
    const response = await invokeDirectRaw(path, {
      ...options,
      accept: "application/json",
    });
    return parseJsonBody(await response.text(), `${method} ${path}`);
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
    getHandshake: () => invokeDirectJSON("/meta/handshake"),

    createActor: (payload) =>
      invokeDirectJSON("/actors", {
        method: "POST",
        body: JSON.stringify(payload),
        headers: {
          "content-type": "application/json",
        },
      }),
    listActors: (filters) => invokeDirectJSON("/actors", { query: filters }),
    issueAuthToken: (payload) =>
      invokeDirectJSON("/auth/token", {
        method: "POST",
        body: JSON.stringify(payload),
        headers: { "content-type": "application/json" },
      }),
    getCurrentAgent: () => invokeDirectJSON("/agents/me"),
    passkeyRegisterOptions: (payload) =>
      invokeDirectJSON("/auth/passkey/register/options", {
        method: "POST",
        body: JSON.stringify(payload),
        headers: { "content-type": "application/json" },
      }),
    passkeyRegisterVerify: (payload) =>
      invokeDirectJSON("/auth/passkey/register/verify", {
        method: "POST",
        body: JSON.stringify(payload),
        headers: { "content-type": "application/json" },
      }),
    passkeyLoginOptions: (payload) =>
      invokeDirectJSON("/auth/passkey/login/options", {
        method: "POST",
        body: JSON.stringify(payload),
        headers: { "content-type": "application/json" },
      }),
    passkeyLoginVerify: (payload) =>
      invokeDirectJSON("/auth/passkey/login/verify", {
        method: "POST",
        body: JSON.stringify(payload),
        headers: { "content-type": "application/json" },
      }),
    bootstrapStatus: () => invokeDirectJSON("/auth/bootstrap/status"),
    listInvites: () => invokeDirectJSON("/auth/invites"),
    createInvite: (payload) =>
      invokeDirectJSON("/auth/invites", {
        method: "POST",
        body: JSON.stringify(payload),
        headers: { "content-type": "application/json" },
      }),
    revokeInvite: (inviteId) =>
      invokeDirectJSON(
        `/auth/invites/${encodeURIComponent(String(inviteId))}/revoke`,
        {
          method: "POST",
          body: JSON.stringify({}),
          headers: { "content-type": "application/json" },
        },
      ),
    listPrincipals: (filters) =>
      invokeDirectJSON("/auth/principals", { query: filters }),
    revokePrincipal: (agentId, payload = {}) =>
      invokeDirectJSON(
        `/auth/principals/${encodeURIComponent(String(agentId))}/revoke`,
        {
          method: "POST",
          body: JSON.stringify(payload),
          headers: { "content-type": "application/json" },
        },
      ),
    listAuthAudit: (filters) =>
      invokeDirectJSON("/auth/audit", { query: filters }),

    createThread: (payload) =>
      invokeDirectJSON("/threads", {
        method: "POST",
        body: JSON.stringify(withActorId(payload)),
        headers: { "content-type": "application/json" },
      }),
    listThreads: (filters) =>
      invokeJSON("threads.list", () =>
        generated.threadsList({ query: filters }),
      ),
    archiveThread: (threadId, payload) =>
      invokeDirectJSON(
        `/threads/${encodeURIComponent(String(threadId))}/archive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    unarchiveThread: (threadId, payload) =>
      invokeDirectJSON(
        `/threads/${encodeURIComponent(String(threadId))}/unarchive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    tombstoneThread: (threadId, payload) =>
      invokeDirectJSON(
        `/threads/${encodeURIComponent(String(threadId))}/tombstone`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    restoreThread: (threadId, payload) =>
      invokeDirectJSON(
        `/threads/${encodeURIComponent(String(threadId))}/restore`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    purgeThread: (threadId, payload) =>
      invokeDirectJSON(
        `/threads/${encodeURIComponent(String(threadId))}/purge`,
        {
          method: "POST",
          body: JSON.stringify(payload || {}),
          headers: { "content-type": "application/json" },
        },
      ),
    getThread: (threadId) =>
      invokeJSON("threads.inspect", () =>
        generated.threadsInspect({ thread_id: String(threadId) }),
      ),
    updateThread: (threadId, payload) =>
      invokeDirectJSON(`/threads/${encodeURIComponent(String(threadId))}`, {
        method: "PATCH",
        body: JSON.stringify(withActorId(payload)),
        headers: { "content-type": "application/json" },
      }),
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
      const response = await invokeDirectRaw("/events/stream", {
        query: {
          thread_id: String(threadId),
          last_event_id: lastEventId,
        },
        accept: "text/event-stream",
        signal,
      });
      await consumeSSEStream(response, { onEvent, signal });
    },
    // Snapshot refs (`snapshot:{thread_id}`) resolve to backing thread state; there is no GET /snapshots in 0.3.x.
    getSnapshot: async (snapshotId) => {
      const body = await invokeJSON("threads.inspect", () =>
        generated.threadsInspect({ thread_id: String(snapshotId) }),
      );
      return { snapshot: body.thread ?? null };
    },

    listTopics: (filters) =>
      invokeJSON("topics.list", () => generated.topicsList({ query: filters })),
    createTopic: (payload) =>
      invokeJSON("topics.create", () =>
        generated.topicsCreate({ body: withActorId(payload) }),
      ),
    getTopic: (topicId) =>
      invokeJSON("topics.get", () =>
        generated.topicsGet({ topic_id: String(topicId) }),
      ),
    updateTopic: (topicId, payload) =>
      invokeJSON("topics.patch", () =>
        generated.topicsPatch(
          { topic_id: String(topicId) },
          { body: withActorId(payload) },
        ),
      ),
    listCards: (filters) =>
      invokeJSON("cards.list", () => generated.cardsList({ query: filters })),
    getCard: (cardId) =>
      invokeJSON("cards.get", () =>
        generated.cardsGet({ card_id: String(cardId) }),
      ),
    archiveCard: (cardId, payload) =>
      invokeDirectJSON(`/cards/${encodeURIComponent(String(cardId))}/archive`, {
        method: "POST",
        body: JSON.stringify(withActorId(payload)),
        headers: {
          "content-type": "application/json",
        },
      }),
    restoreCard: (cardId, payload) =>
      invokeDirectJSON(`/cards/${encodeURIComponent(String(cardId))}/restore`, {
        method: "POST",
        body: JSON.stringify(withActorId(payload)),
        headers: {
          "content-type": "application/json",
        },
      }),
    purgeCard: (cardId, payload) =>
      invokeDirectJSON(`/cards/${encodeURIComponent(String(cardId))}/purge`, {
        method: "POST",
        body: JSON.stringify(withActorId(payload)),
        headers: {
          "content-type": "application/json",
        },
      }),

    createArtifact: (payload) =>
      invokeDirectJSON("/artifacts", {
        method: "POST",
        body: JSON.stringify(withActorId(payload)),
        headers: { "content-type": "application/json" },
      }),
    listArtifacts: (filters) =>
      invokeDirectJSON("/artifacts", { query: filters }),
    getArtifact: (artifactId) =>
      invokeJSON("artifacts.get", () =>
        generated.artifactsGet({ artifact_id: String(artifactId) }),
      ),
    archiveArtifact: (artifactId, payload) =>
      invokeDirectJSON(
        `/artifacts/${encodeURIComponent(String(artifactId))}/archive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    unarchiveArtifact: (artifactId, payload) =>
      invokeDirectJSON(
        `/artifacts/${encodeURIComponent(String(artifactId))}/unarchive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    tombstoneArtifact: (artifactId, payload) =>
      invokeDirectJSON(
        `/artifacts/${encodeURIComponent(String(artifactId))}/tombstone`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    restoreArtifact: (artifactId, payload) =>
      invokeDirectJSON(
        `/artifacts/${encodeURIComponent(String(artifactId))}/restore`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    purgeArtifact: (artifactId, payload) =>
      invokeDirectJSON(
        `/artifacts/${encodeURIComponent(String(artifactId))}/purge`,
        {
          method: "POST",
          body: JSON.stringify(payload || {}),
          headers: { "content-type": "application/json" },
        },
      ),
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
      invokeJSON("docs.revisions.list", () =>
        generated.docsRevisionsList({ document_id: String(documentId) }),
      ),
    getDocumentRevision: (documentId, revisionId) =>
      invokeJSON("docs.revisions.get", () =>
        generated.docsRevisionsGet({
          document_id: String(documentId),
          revision_id: String(revisionId),
        }),
      ),
    updateDocument: (documentId, payload) =>
      invokeDirectJSON(`/docs/${encodeURIComponent(String(documentId))}`, {
        method: "PATCH",
        body: JSON.stringify(withActorId(payload)),
        headers: { "content-type": "application/json" },
      }),
    tombstoneDocument: (documentId, payload) =>
      invokeDirectJSON(
        `/docs/${encodeURIComponent(String(documentId))}/tombstone`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    archiveDocument: (documentId, payload) =>
      invokeDirectJSON(
        `/docs/${encodeURIComponent(String(documentId))}/archive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    unarchiveDocument: (documentId, payload) =>
      invokeDirectJSON(
        `/docs/${encodeURIComponent(String(documentId))}/unarchive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    restoreDocument: (documentId, payload) =>
      invokeDirectJSON(
        `/docs/${encodeURIComponent(String(documentId))}/restore`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    purgeDocument: (documentId, payload) =>
      invokeDirectJSON(
        `/docs/${encodeURIComponent(String(documentId))}/purge`,
        {
          method: "POST",
          body: JSON.stringify(payload || {}),
          headers: { "content-type": "application/json" },
        },
      ),

    createEvent: (payload) =>
      invokeJSON("events.create", () =>
        generated.eventsCreate({ body: withActorId(payload) }),
      ),
    listEvents: (filters) =>
      invokeJSON("events.list", () => generated.eventsList({ query: filters })),
    getEvent: (eventId) =>
      invokeDirectJSON(`/events/${encodeURIComponent(String(eventId))}`),
    archiveEvent: (eventId, payload) =>
      invokeDirectJSON(
        `/events/${encodeURIComponent(String(eventId))}/archive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    unarchiveEvent: (eventId, payload) =>
      invokeDirectJSON(
        `/events/${encodeURIComponent(String(eventId))}/unarchive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    tombstoneEvent: (eventId, payload) =>
      invokeDirectJSON(
        `/events/${encodeURIComponent(String(eventId))}/tombstone`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),
    restoreEvent: (eventId, payload) =>
      invokeDirectJSON(
        `/events/${encodeURIComponent(String(eventId))}/restore`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: { "content-type": "application/json" },
        },
      ),

    createWorkOrder: (payload) => {
      const body = withActorId({ ...payload });
      if (body.packet) {
        body.packet = normalizePacketThreadIdToSubjectRef(body.packet);
      }
      return invokeJSON("packets.work-orders.create", () =>
        generated.packetsWorkOrdersCreate({ body }),
      );
    },
    createReceipt: (payload) => {
      const body = withActorId({ ...payload });
      if (body.packet) {
        body.packet = normalizePacketThreadIdToSubjectRef(body.packet);
      }
      return invokeJSON("packets.receipts.create", () =>
        generated.packetsReceiptsCreate({ body }),
      );
    },
    createReview: (payload) => {
      const body = withActorId({ ...payload });
      if (body.packet) {
        body.packet = normalizePacketThreadIdToSubjectRef(body.packet);
      }
      return invokeJSON("packets.reviews.create", () =>
        generated.packetsReviewsCreate({ body }),
      );
    },

    listInboxItems: (filters) =>
      invokeJSON("inbox.list", () => generated.inboxList({ query: filters })),
    ackInboxItem: (payload) => {
      const inboxItemId =
        payload?.inbox_item_id ?? payload?.inbox_id ?? payload?.inboxId;
      if (!inboxItemId) {
        throw new Error(
          "ackInboxItem requires inbox_item_id (or inbox_id) in the payload.",
        );
      }
      const body = { ...(payload ?? {}) };
      delete body.inbox_item_id;
      delete body.inbox_id;
      delete body.inboxId;
      return invokeJSON("inbox.acknowledge", () =>
        generated.inboxAcknowledge(
          { inbox_id: String(inboxItemId) },
          { body: withActorId(body) },
        ),
      );
    },

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
      invokeDirectJSON(`/boards/${encodeURIComponent(String(boardId))}`, {
        method: "PATCH",
        body: JSON.stringify(withActorId(payload)),
        headers: {
          "content-type": "application/json",
        },
      }),
    getBoardWorkspace: (boardId) =>
      invokeJSON("boards.workspace", () =>
        generated.boardsWorkspace({ board_id: String(boardId) }),
      ),
    archiveBoard: (boardId, payload) =>
      invokeDirectJSON(
        `/boards/${encodeURIComponent(String(boardId))}/archive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    unarchiveBoard: (boardId, payload) =>
      invokeDirectJSON(
        `/boards/${encodeURIComponent(String(boardId))}/unarchive`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    tombstoneBoard: (boardId, payload) =>
      invokeDirectJSON(
        `/boards/${encodeURIComponent(String(boardId))}/tombstone`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    restoreBoard: (boardId, payload) =>
      invokeDirectJSON(
        `/boards/${encodeURIComponent(String(boardId))}/restore`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    purgeBoard: (boardId, payload) =>
      invokeDirectJSON(`/boards/${encodeURIComponent(String(boardId))}/purge`, {
        method: "POST",
        body: JSON.stringify(payload || {}),
        headers: {
          "content-type": "application/json",
        },
      }),

    addBoardCard: (boardId, payload) =>
      invokeJSON("boards.cards.create", () =>
        generated.boardsCardsCreate(
          { board_id: String(boardId) },
          { body: withActorId(payload) },
        ),
      ),
    listBoardCards: (boardId) =>
      invokeJSON("boards.cards.list", () =>
        generated.boardsCardsList({ board_id: String(boardId) }),
      ),
    moveBoardCard: (boardId, cardId, payload) =>
      invokeDirectJSON(
        `/boards/${encodeURIComponent(String(boardId))}/cards/${encodeURIComponent(String(cardId))}/move`,
        {
          method: "POST",
          body: JSON.stringify(withActorId(payload)),
          headers: {
            "content-type": "application/json",
          },
        },
      ),
    removeBoardCard: (boardId, cardId, payload) =>
      invokeDirectJSON(`/cards/${encodeURIComponent(String(cardId))}/archive`, {
        method: "POST",
        body: JSON.stringify(withActorId(payload)),
        headers: {
          "content-type": "application/json",
        },
      }),
    updateBoardCard: (boardId, cardId, payload) =>
      invokeDirectJSON(`/cards/${encodeURIComponent(String(cardId))}`, {
        method: "PATCH",
        body: JSON.stringify(withActorId(payload)),
        headers: {
          "content-type": "application/json",
        },
      }),
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
