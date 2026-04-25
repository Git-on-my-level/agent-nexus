import { normalizeBaseUrl } from "$lib/config.js";
import { readHostedControlPlaneAccessToken } from "./cpSessionCookie.js";
import {
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths.js";

function controlPlaneAccessToken(env, event) {
  return readHostedControlPlaneAccessToken(event, env ?? {});
}

async function readResponseBody(response) {
  if (response && typeof response.text === "function") {
    const text = String(await response.text().catch(() => "")).trim();
    if (!text) {
      return {};
    }
    try {
      return JSON.parse(text);
    } catch {
      return { message: text };
    }
  }
  if (response && typeof response.json === "function") {
    try {
      return await response.json();
    } catch {
      return {};
    }
  }
  return {};
}

async function requestJSON(event, url, init = {}) {
  const fetchFn = event?.fetch ?? fetch;
  try {
    const response = await fetchFn(url, init);
    const body = await readResponseBody(response);
    const status = Number(response?.status ?? 0);
    return {
      ok: Boolean(response?.ok),
      status,
      body,
      headers: new Headers(response?.headers ?? undefined),
    };
  } catch (error) {
    const message =
      error instanceof Error ? error.message : String(error ?? "network_error");
    return {
      ok: false,
      status: 503,
      body: {
        error: {
          code: "network_error",
          message,
        },
      },
      headers: new Headers(),
    };
  }
}

function bearerHeaders(token, base = {}) {
  const headers = new Headers(base);
  if (token) {
    headers.set("authorization", `Bearer ${token}`);
  }
  return headers;
}

function isUnauthorizedStatus(status) {
  return status === 401 || status === 403;
}

const REQUEST_CACHE_KEY = "__anxOutOfWorkspaceCpClientCache";

function requestCache(event) {
  if (!event?.locals || typeof event.locals !== "object") {
    return null;
  }
  if (!event.locals[REQUEST_CACHE_KEY]) {
    event.locals[REQUEST_CACHE_KEY] = {
      orgBySlug: new Map(),
      workspacesByOrganizationId: new Map(),
      workspaceBySlug: new Map(),
      workspaceById: new Map(),
    };
  }
  return event.locals[REQUEST_CACHE_KEY];
}

export function createControlPlaneClient({ controlPlaneBaseUrl, env }) {
  const base = normalizeBaseUrl(
    controlPlaneBaseUrl ?? env?.ANX_CONTROL_BASE_URL,
  );

  async function resolveOrganizationIdBySlug({ event, organizationSlug }) {
    const token = controlPlaneAccessToken(env, event);
    const normalizedOrg = normalizeOrganizationSlug(organizationSlug);
    if (!base || !normalizedOrg) {
      return { kind: "missing" };
    }
    if (!token) {
      return { kind: "unauthenticated" };
    }

    const cache = requestCache(event);
    const cacheKey = `${token}\n${normalizedOrg}`;
    const cached = cache?.orgBySlug.get(cacheKey);
    if (cached) {
      return cached;
    }

    let cursor = "";
    for (let page = 0; page < 20; page += 1) {
      const url = new URL("/organizations", `${base}/`);
      url.searchParams.set("limit", "200");
      if (cursor) {
        url.searchParams.set("cursor", cursor);
      }
      const result = await requestJSON(event, url.toString(), {
        headers: bearerHeaders(token),
      });
      if (!result.ok) {
        if (isUnauthorizedStatus(result.status)) {
          return { kind: "unauthenticated" };
        }
        return { kind: "missing" };
      }
      const organizations = Array.isArray(result.body?.organizations)
        ? result.body.organizations
        : [];
      const match = organizations.find(
        (row) => normalizeOrganizationSlug(row?.slug) === normalizedOrg,
      );
      if (match) {
        const found = {
          kind: "found",
          organizationId: String(match.id ?? "").trim(),
        };
        cache?.orgBySlug.set(cacheKey, found);
        return found;
      }
      cursor = String(result.body?.next_cursor ?? "").trim();
      if (!cursor) {
        break;
      }
    }

    const missing = { kind: "missing" };
    cache?.orgBySlug.set(cacheKey, missing);
    return missing;
  }

  async function listWorkspacesByOrganizationId({ event, organizationId }) {
    const token = controlPlaneAccessToken(env, event);
    const orgId = String(organizationId ?? "").trim();
    if (!base || !orgId) {
      return { kind: "ok", workspaces: [] };
    }
    if (!token) {
      return { kind: "unauthenticated", workspaces: [] };
    }

    const cache = requestCache(event);
    const cacheKey = `${token}\n${orgId}`;
    const cached = cache?.workspacesByOrganizationId.get(cacheKey);
    if (cached) {
      return cached;
    }

    /** @type {unknown[]} */
    const out = [];
    let cursor = "";
    for (let page = 0; page < 20; page += 1) {
      const url = new URL("/workspaces", `${base}/`);
      url.searchParams.set("organization_id", orgId);
      url.searchParams.set("limit", "200");
      if (cursor) {
        url.searchParams.set("cursor", cursor);
      }
      const result = await requestJSON(event, url.toString(), {
        headers: bearerHeaders(token),
      });
      if (!result.ok) {
        if (isUnauthorizedStatus(result.status)) {
          const unauthenticated = { kind: "unauthenticated", workspaces: [] };
          cache?.workspacesByOrganizationId.set(cacheKey, unauthenticated);
          return unauthenticated;
        }
        const empty = { kind: "ok", workspaces: [] };
        cache?.workspacesByOrganizationId.set(cacheKey, empty);
        return empty;
      }
      const batch = Array.isArray(result.body?.workspaces)
        ? result.body.workspaces
        : [];
      out.push(...batch);
      cursor = String(result.body?.next_cursor ?? "").trim();
      if (!cursor) {
        break;
      }
    }
    const ok = { kind: "ok", workspaces: out };
    cache?.workspacesByOrganizationId.set(cacheKey, ok);
    return ok;
  }

  async function findWorkspaceBySlug({
    event,
    organizationSlug,
    workspaceSlug,
  }) {
    const normalizedWorkspace = normalizeWorkspaceSlug(workspaceSlug);
    if (!normalizedWorkspace) {
      return { kind: "missing" };
    }
    const normalizedOrg = normalizeOrganizationSlug(organizationSlug);
    if (!normalizedOrg) {
      return { kind: "missing" };
    }
    const token = controlPlaneAccessToken(env, event);
    const cache = requestCache(event);
    const cacheKey = `${token}\n${normalizedOrg}\n${normalizedWorkspace}`;
    const cached = cache?.workspaceBySlug.get(cacheKey);
    if (cached) {
      return cached;
    }

    const orgLookup = await resolveOrganizationIdBySlug({
      event,
      organizationSlug: normalizedOrg,
    });
    if (orgLookup.kind !== "found") {
      cache?.workspaceBySlug.set(cacheKey, orgLookup);
      return orgLookup;
    }

    const workspaces = await listWorkspacesByOrganizationId({
      event,
      organizationId: orgLookup.organizationId,
    });
    if (workspaces.kind === "unauthenticated") {
      const unauthenticated = { kind: "unauthenticated" };
      cache?.workspaceBySlug.set(cacheKey, unauthenticated);
      return unauthenticated;
    }
    const match = workspaces.workspaces.find(
      (row) => normalizeWorkspaceSlug(row?.slug) === normalizedWorkspace,
    );
    if (!match) {
      const missing = { kind: "missing" };
      cache?.workspaceBySlug.set(cacheKey, missing);
      return missing;
    }
    const found = { kind: "found", workspace: match };
    cache?.workspaceBySlug.set(cacheKey, found);
    return found;
  }

  async function getWorkspaceById({ event, workspaceId }) {
    const token = controlPlaneAccessToken(env, event);
    const wsId = String(workspaceId ?? "").trim();
    if (!base || !wsId) {
      return { kind: "missing" };
    }
    if (!token) {
      return { kind: "unauthenticated" };
    }

    const cache = requestCache(event);
    const cacheKey = `${token}\n${wsId}`;
    const cached = cache?.workspaceById.get(cacheKey);
    if (cached) {
      return cached;
    }

    const url = new URL(`/workspaces/${encodeURIComponent(wsId)}`, `${base}/`);
    const result = await requestJSON(event, url.toString(), {
      headers: bearerHeaders(token),
    });
    if (!result.ok) {
      if (isUnauthorizedStatus(result.status)) {
        const unauthenticated = { kind: "unauthenticated" };
        cache?.workspaceById.set(cacheKey, unauthenticated);
        return unauthenticated;
      }
      const missing = { kind: "missing" };
      cache?.workspaceById.set(cacheKey, missing);
      return missing;
    }
    if (!result.body?.workspace || typeof result.body.workspace !== "object") {
      const missing = { kind: "missing" };
      cache?.workspaceById.set(cacheKey, missing);
      return missing;
    }
    const found = { kind: "found", workspace: result.body.workspace };
    cache?.workspaceById.set(cacheKey, found);
    return found;
  }

  async function createLaunchSession({ event, workspaceId, returnPath }) {
    const token = controlPlaneAccessToken(env, event);
    const wsId = String(workspaceId ?? "").trim();
    if (!base || !wsId) {
      return {
        ok: false,
        status: 400,
        body: {
          error: {
            code: "invalid_request",
            message: "workspace_id is required.",
          },
        },
      };
    }
    if (!token) {
      return {
        ok: false,
        status: 401,
        body: {
          error: {
            code: "control_plane_unauthenticated",
            message: "Missing control-plane session.",
          },
        },
      };
    }
    const url = new URL(
      `/workspaces/${encodeURIComponent(wsId)}/launch-sessions`,
      `${base}/`,
    );
    return requestJSON(event, url.toString(), {
      method: "POST",
      headers: bearerHeaders(token, {
        accept: "application/json",
        "content-type": "application/json",
      }),
      body: JSON.stringify({
        return_path: String(returnPath ?? "").trim() || "/",
      }),
    });
  }

  async function exchangeLaunchSession({
    event,
    workspaceId,
    exchangeToken,
    state,
  }) {
    const wsId = String(workspaceId ?? "").trim();
    if (!base || !wsId) {
      return {
        ok: false,
        status: 400,
        body: {
          error: {
            code: "invalid_request",
            message: "workspace_id is required.",
          },
        },
      };
    }
    const url = new URL(
      `/workspaces/${encodeURIComponent(wsId)}/session-exchange`,
      `${base}/`,
    );
    return requestJSON(event, url.toString(), {
      method: "POST",
      headers: {
        accept: "application/json",
        "content-type": "application/json",
      },
      body: JSON.stringify({
        exchange_token: String(exchangeToken ?? "").trim(),
        state: String(state ?? "").trim(),
      }),
    });
  }

  return {
    base,
    getAccessToken(event) {
      return controlPlaneAccessToken(env, event);
    },
    resolveOrganizationIdBySlug,
    listWorkspacesByOrganizationId,
    findWorkspaceBySlug,
    getWorkspaceById,
    createLaunchSession,
    exchangeLaunchSession,
  };
}
