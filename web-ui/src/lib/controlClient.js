import { normalizeBaseUrl } from "./config.js";

function resolveFetch(fetchFn) {
  if (typeof fetchFn === "function") {
    return fetchFn;
  }
  return globalThis.fetch.bind(globalThis);
}

function buildUrl(pathname, baseUrl = "") {
  const resolvedBaseUrl = normalizeBaseUrl(baseUrl);
  if (!resolvedBaseUrl) {
    return pathname;
  }
  return new URL(pathname, `${resolvedBaseUrl}/`).toString();
}

function createErrorFromResponse(status, details) {
  const message =
    details?.error?.message || details?.message || `request failed (${status})`;
  const error = new Error(message);
  error.status = status;
  error.details = details;
  return error;
}

async function requestJSON(
  pathname,
  { fetchFn, method = "GET", body, baseUrl = "", headers } = {},
) {
  const response = await resolveFetch(fetchFn)(buildUrl(pathname, baseUrl), {
    method,
    headers: {
      accept: "application/json",
      ...(body ? { "content-type": "application/json" } : {}),
      ...(headers ?? {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  const rawText = await response.text();
  let payload = {};
  if (rawText) {
    try {
      payload = JSON.parse(rawText);
    } catch {
      payload = { message: rawText };
    }
  }
  if (!response.ok) {
    throw createErrorFromResponse(response.status, payload);
  }

  return payload;
}

function controlApiPath(pathname) {
  return `/control/api${pathname}`;
}

export const controlClient = {
  async startPasskeyRegistration(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/auth", {
      fetchFn,
      baseUrl,
      method: "POST",
      body: {
        action: "register-start",
        ...body,
      },
    });
  },

  async finishPasskeyRegistration(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/auth", {
      fetchFn,
      baseUrl,
      method: "POST",
      body: {
        action: "register-finish",
        ...body,
      },
    });
  },

  async startSession(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/auth", {
      fetchFn,
      baseUrl,
      method: "POST",
      body: {
        action: "login-start",
        ...body,
      },
    });
  },

  async finishSession(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/auth", {
      fetchFn,
      baseUrl,
      method: "POST",
      body: {
        action: "login-finish",
        ...body,
      },
    });
  },

  async revokeCurrentSession({ fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/auth", {
      fetchFn,
      baseUrl,
      method: "DELETE",
    });
  },

  async listOrganizations({ fetchFn, baseUrl = "" } = {}) {
    return requestJSON(controlApiPath("/organizations"), {
      fetchFn,
      baseUrl,
    });
  },

  async createOrganization(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON(controlApiPath("/organizations"), {
      fetchFn,
      baseUrl,
      method: "POST",
      body,
    });
  },

  async getOrganization(organizationId, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON(
      controlApiPath(`/organizations/${encodeURIComponent(organizationId)}`),
      {
        fetchFn,
        baseUrl,
      },
    );
  },

  async listOrganizationInvites(
    organizationId,
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      controlApiPath(
        `/organizations/${encodeURIComponent(organizationId)}/invites`,
      ),
      {
        fetchFn,
        baseUrl,
      },
    );
  },

  async listWorkspaces(organizationId, { fetchFn, baseUrl = "" } = {}) {
    const query = organizationId
      ? `?organization_id=${encodeURIComponent(organizationId)}`
      : "";
    return requestJSON(controlApiPath(`/workspaces${query}`), {
      fetchFn,
      baseUrl,
    });
  },

  async createWorkspace(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON(controlApiPath("/workspaces"), {
      fetchFn,
      baseUrl,
      method: "POST",
      body,
    });
  },

  async launchWorkspace(
    workspaceId,
    workspaceSlug,
    { fetchFn, baseUrl = "", returnPath = "/" } = {},
  ) {
    return requestJSON("/dashboard/launch", {
      fetchFn,
      baseUrl,
      method: "POST",
      body: {
        workspace_id: workspaceId,
        workspace_slug: workspaceSlug,
        return_path: returnPath,
      },
    });
  },
};
