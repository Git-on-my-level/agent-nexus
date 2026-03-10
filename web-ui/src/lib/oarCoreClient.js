import {
  OarClient,
  commandRegistry,
} from "../../../contracts/gen/ts/dist/client.js";

import { EXPECTED_SCHEMA_VERSION, normalizeBaseUrl } from "./config.js";
import { appPath } from "./projectPaths.js";

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

async function parseRawErrorResponse(response, fallbackStatusText) {
  const rawDetails = await response.text().catch(() => "");
  const details = extractErrorMessage(rawDetails);
  const detailSuffix = details ? ` - ${details}` : "";
  const guidanceSuffix =
    response.status >= 500
      ? " oar-core may be unavailable; verify backend startup and base URL."
      : "";

  const requestError = new Error(
    `oar-core request failed: (${response.status} ${fallbackStatusText})${detailSuffix}${guidanceSuffix}`,
  );
  requestError.status = response.status;
  requestError.details = details;
  throw requestError;
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

  async function invokeRaw(commandId, pathParams = {}) {
    const command = commandInfo(commandId);
    const resolvedPath = renderPath(command.path, pathParams);
    const url = toAbsoluteUrl(resolvedBaseUrl, resolvedPath);

    let response;
    try {
      response = await fetchFn(url, {
        method: command.method,
        headers: {
          accept: "*/*",
        },
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
      try {
        await parseRawErrorResponse(response, response.statusText);
      } catch (error) {
        if (error instanceof Error) {
          const wrapped = new Error(
            `oar-core request failed at ${target}: ${command.method} ${command.path} (${response.status})${error.details ? ` - ${error.details}` : ""}${response.status >= 500 ? " oar-core may be unavailable; verify backend startup and base URL." : ""}`,
          );
          wrapped.status = response.status;
          wrapped.details = error.details;
          throw wrapped;
        }
        throw error;
      }
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
    listActors: () => invokeJSON("actors.list", () => generated.actorsList()),
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

    createThread: (payload) =>
      invokeJSON("threads.create", () =>
        generated.threadsCreate({ body: withActorId(payload) }),
      ),
    listThreads: (filters) =>
      invokeJSON("threads.list", () =>
        generated.threadsList({ query: filters }),
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
    listThreadTimeline: (threadId) =>
      invokeJSON("threads.timeline", () =>
        generated.threadsTimeline({ thread_id: String(threadId) }),
      ),
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

    createEvent: (payload) =>
      invokeJSON("events.create", () =>
        generated.eventsCreate({ body: withActorId(payload) }),
      ),
    getEvent: (eventId) =>
      invokeJSON("events.get", () =>
        generated.eventsGet({ event_id: String(eventId) }),
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
  };
}

export async function verifyCoreSchemaVersion(
  client,
  expectedSchemaVersion = EXPECTED_SCHEMA_VERSION,
) {
  const target = client.baseUrl || "same-origin";

  let version;
  try {
    version = await client.getHandshake();
  } catch (error) {
    if (error?.status === 404) {
      try {
        version = await client.getVersion();
      } catch (fallbackError) {
        const reason =
          fallbackError instanceof Error
            ? fallbackError.message
            : String(fallbackError);
        throw new Error(
          `Unable to verify oar-core schema version at ${target}: ${reason}`,
        );
      }
    } else {
      const reason = error instanceof Error ? error.message : String(error);
      throw new Error(
        `Unable to verify oar-core schema version at ${target}: ${reason}`,
      );
    }
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

  return version;
}
