import { normalizeBaseUrl } from "./config.js";

const browser = typeof window !== "undefined";

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
  { fetchFn, method = "GET", body, baseUrl, headers } = {},
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

function getStoredAccessToken() {
  if (!browser) return "";
  try {
    return localStorage.getItem("oar_control_access_token") || "";
  } catch {
    return "";
  }
}

function setStoredAccessToken(token) {
  if (!browser) return;
  try {
    if (token) {
      localStorage.setItem("oar_control_access_token", token);
    } else {
      localStorage.removeItem("oar_control_access_token");
    }
  } catch {
    // ignore
  }
}

function authHeaders() {
  const token = getStoredAccessToken();
  return token ? { authorization: `Bearer ${token}` } : {};
}

export const controlClient = {
  async startPasskeyRegistration(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/account/passkeys/registrations/start", {
      fetchFn,
      baseUrl,
      method: "POST",
      body,
    });
  },

  async finishPasskeyRegistration(body, { fetchFn, baseUrl = "" } = {}) {
    const result = await requestJSON("/account/passkeys/registrations/finish", {
      fetchFn,
      baseUrl,
      method: "POST",
      body,
    });
    if (result.session?.access_token) {
      setStoredAccessToken(result.session.access_token);
    }
    return result;
  },

  async startSession(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/account/sessions/start", {
      fetchFn,
      baseUrl,
      method: "POST",
      body,
    });
  },

  async finishSession(body, { fetchFn, baseUrl = "" } = {}) {
    const result = await requestJSON("/account/sessions/finish", {
      fetchFn,
      baseUrl,
      method: "POST",
      body,
    });
    if (result.session?.access_token) {
      setStoredAccessToken(result.session.access_token);
    }
    return result;
  },

  async revokeCurrentSession({ fetchFn, baseUrl = "" } = {}) {
    try {
      await requestJSON("/account/sessions/current", {
        fetchFn,
        baseUrl,
        method: "DELETE",
        headers: authHeaders(),
      });
    } finally {
      setStoredAccessToken("");
    }
  },

  async listOrganizations({ fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/organizations", {
      fetchFn,
      baseUrl,
      headers: authHeaders(),
    });
  },

  async createOrganization(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/organizations", {
      fetchFn,
      baseUrl,
      method: "POST",
      body,
      headers: authHeaders(),
    });
  },

  async getOrganization(organizationId, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON(`/organizations/${encodeURIComponent(organizationId)}`, {
      fetchFn,
      baseUrl,
      headers: authHeaders(),
    });
  },

  async listWorkspaces(organizationId, { fetchFn, baseUrl = "" } = {}) {
    const query = organizationId
      ? `?organization_id=${encodeURIComponent(organizationId)}`
      : "";
    return requestJSON(`/workspaces${query}`, {
      fetchFn,
      baseUrl,
      headers: authHeaders(),
    });
  },

  async createWorkspace(body, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON("/workspaces", {
      fetchFn,
      baseUrl,
      method: "POST",
      body,
      headers: authHeaders(),
    });
  },

  async getWorkspace(workspaceId, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON(`/workspaces/${encodeURIComponent(workspaceId)}`, {
      fetchFn,
      baseUrl,
      headers: authHeaders(),
    });
  },

  async getProvisioningJob(jobId, { fetchFn, baseUrl = "" } = {}) {
    return requestJSON(`/provisioning/jobs/${encodeURIComponent(jobId)}`, {
      fetchFn,
      baseUrl,
      headers: authHeaders(),
    });
  },

  async createLaunchSession(
    workspaceId,
    body = {},
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      `/workspaces/${encodeURIComponent(workspaceId)}/launch-sessions`,
      {
        fetchFn,
        baseUrl,
        method: "POST",
        body,
        headers: authHeaders(),
      },
    );
  },

  async exchangeWorkspaceSession(
    workspaceId,
    exchangeToken,
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      `/workspaces/${encodeURIComponent(workspaceId)}/session-exchange`,
      {
        fetchFn,
        baseUrl,
        method: "POST",
        body: { exchange_token: exchangeToken },
      },
    );
  },

  async listOrganizationMemberships(
    organizationId,
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      `/organizations/${encodeURIComponent(organizationId)}/memberships`,
      {
        fetchFn,
        baseUrl,
        headers: authHeaders(),
      },
    );
  },

  async listOrganizationInvites(
    organizationId,
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      `/organizations/${encodeURIComponent(organizationId)}/invites`,
      {
        fetchFn,
        baseUrl,
        headers: authHeaders(),
      },
    );
  },

  async createOrganizationInvite(
    organizationId,
    body,
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      `/organizations/${encodeURIComponent(organizationId)}/invites`,
      {
        fetchFn,
        baseUrl,
        method: "POST",
        body,
        headers: authHeaders(),
      },
    );
  },

  async revokeOrganizationInvite(
    organizationId,
    inviteId,
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      `/organizations/${encodeURIComponent(organizationId)}/invites/${encodeURIComponent(inviteId)}/revoke`,
      {
        fetchFn,
        baseUrl,
        method: "POST",
        headers: authHeaders(),
      },
    );
  },

  async acceptOrganizationInvite(
    organizationId,
    inviteId,
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      `/organizations/${encodeURIComponent(organizationId)}/invites/${encodeURIComponent(inviteId)}/accept`,
      {
        fetchFn,
        baseUrl,
        method: "POST",
        headers: authHeaders(),
      },
    );
  },

  async getOrganizationUsageSummary(
    organizationId,
    { fetchFn, baseUrl = "" } = {},
  ) {
    return requestJSON(
      `/organizations/${encodeURIComponent(organizationId)}/usage-summary`,
      {
        fetchFn,
        baseUrl,
        headers: authHeaders(),
      },
    );
  },

  clearStoredToken() {
    setStoredAccessToken("");
  },
};
