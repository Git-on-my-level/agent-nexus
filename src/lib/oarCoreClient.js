import { EXPECTED_SCHEMA_VERSION, oarCoreBaseUrl } from "./config.js";

function encodePathSegment(value) {
  return encodeURIComponent(String(value));
}

function appendQuery(path, query = {}) {
  const params = new URLSearchParams();

  Object.entries(query).forEach(([key, value]) => {
    if (value === undefined || value === null || value === "") {
      return;
    }

    if (Array.isArray(value)) {
      value
        .filter(
          (entry) => entry !== undefined && entry !== null && entry !== "",
        )
        .forEach((entry) => {
          params.append(key, String(entry));
        });
      return;
    }

    params.set(key, String(value));
  });

  const serialized = params.toString();
  return serialized ? `${path}?${serialized}` : path;
}

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

export function createOarCoreClient(options = {}) {
  const baseUrl = options.baseUrl ?? oarCoreBaseUrl;
  const fetchFn = options.fetchFn ?? fetch;
  const actorIdProvider = options.actorIdProvider;
  const target = baseUrl || "same-origin";

  async function request(method, path, config = {}) {
    const pathWithQuery = appendQuery(path, config.query);
    const url = toAbsoluteUrl(baseUrl, pathWithQuery);

    const headers = {
      accept: "application/json",
      ...config.headers,
    };

    let body;
    if (config.body !== undefined) {
      body = JSON.stringify(config.body);
      headers["content-type"] = "application/json";
    }

    let response;
    try {
      response = await fetchFn(url, { method, headers, body });
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      throw new Error(
        `Unable to reach oar-core at ${target} for ${method} ${path}. Check that oar-core is running and OAR_CORE_BASE_URL is correct. ${reason}`,
      );
    }

    if (!response.ok) {
      const rawDetails = await response.text().catch(() => "");
      const details = extractErrorMessage(rawDetails);
      const detailSuffix = details ? ` - ${details}` : "";
      const guidanceSuffix =
        response.status >= 500
          ? " oar-core may be unavailable; verify backend startup and base URL."
          : "";
      const requestError = new Error(
        `oar-core request failed at ${target}: ${method} ${path} (${response.status} ${response.statusText})${detailSuffix}${guidanceSuffix}`,
      );
      requestError.status = response.status;
      requestError.details = details;
      throw requestError;
    }

    if (config.responseType === "raw") {
      return response;
    }

    return response.json();
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
    if (payload.actor_id) {
      return payload;
    }

    return { ...payload, actor_id: requireActorId() };
  }

  return {
    baseUrl,
    getVersion: () => request("GET", "/version"),

    createActor: (payload) => request("POST", "/actors", { body: payload }),
    listActors: () => request("GET", "/actors"),

    createThread: (payload) =>
      request("POST", "/threads", { body: withActorId(payload) }),
    listThreads: (filters) => request("GET", "/threads", { query: filters }),
    getThread: (threadId) =>
      request("GET", `/threads/${encodePathSegment(threadId)}`),
    updateThread: (threadId, payload) =>
      request("PATCH", `/threads/${encodePathSegment(threadId)}`, {
        body: withActorId(payload),
      }),
    listThreadTimeline: (threadId) =>
      request("GET", `/threads/${encodePathSegment(threadId)}/timeline`),

    createCommitment: (payload) =>
      request("POST", "/commitments", { body: withActorId(payload) }),
    listCommitments: (filters) =>
      request("GET", "/commitments", { query: filters }),
    getCommitment: (commitmentId) =>
      request("GET", `/commitments/${encodePathSegment(commitmentId)}`),
    updateCommitment: (commitmentId, payload) =>
      request("PATCH", `/commitments/${encodePathSegment(commitmentId)}`, {
        body: withActorId(payload),
      }),

    createArtifact: (payload) =>
      request("POST", "/artifacts", { body: withActorId(payload) }),
    listArtifacts: (filters) =>
      request("GET", "/artifacts", { query: filters }),
    getArtifact: (artifactId) =>
      request("GET", `/artifacts/${encodePathSegment(artifactId)}`),
    getArtifactContent: async (artifactId) => {
      const response = await request(
        "GET",
        `/artifacts/${encodePathSegment(artifactId)}/content`,
        { responseType: "raw" },
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

    createEvent: (payload) =>
      request("POST", "/events", { body: withActorId(payload) }),
    getEvent: (eventId) =>
      request("GET", `/events/${encodePathSegment(eventId)}`),

    createWorkOrder: (payload) =>
      request("POST", "/work_orders", { body: withActorId(payload) }),
    createReceipt: (payload) =>
      request("POST", "/receipts", { body: withActorId(payload) }),
    createReview: (payload) =>
      request("POST", "/reviews", { body: withActorId(payload) }),

    listInboxItems: (filters) => request("GET", "/inbox", { query: filters }),
    ackInboxItem: (payload) =>
      request("POST", "/inbox/ack", { body: withActorId(payload) }),
  };
}

export async function verifyCoreSchemaVersion(
  client,
  expectedSchemaVersion = EXPECTED_SCHEMA_VERSION,
) {
  const target = client.baseUrl || "same-origin";

  let version;
  try {
    version = await client.getVersion();
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    throw new Error(
      `Unable to verify oar-core schema version at ${target}: ${reason}`,
    );
  }

  if (version?.schema_version !== expectedSchemaVersion) {
    throw new Error(
      `oar-core schema mismatch at ${target}: expected ${expectedSchemaVersion}, received ${version?.schema_version ?? "unknown"}.`,
    );
  }
}
