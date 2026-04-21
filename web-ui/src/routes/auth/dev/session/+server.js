import { json } from "@sveltejs/kit";

import { env as privateEnv } from "$env/dynamic/private";
import { readLocalDevIdentityBundle } from "$lib/server/devIdentityBundle.js";
import {
  isRetryableWorkspaceAuthSessionError,
  loadWorkspaceAuthenticatedAgent,
  refreshWorkspaceAuthSession,
  resolveWorkspaceSlugFromEvent,
  writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken,
} from "$lib/server/authSession.js";
import { loadWorkspaceCatalog } from "$lib/server/workspaceCatalog.js";

function allowDevIdentityRoutes() {
  if (privateEnv.NODE_ENV === "production") {
    return false;
  }
  const catalog = loadWorkspaceCatalog(privateEnv);
  return catalog.devActorMode === true;
}

/**
 * Call core's POST /auth/passkey/dev/login to obtain fresh tokens without
 * needing the original (possibly consumed) refresh token from the bundle.
 */
async function devLoginForFreshTokens(coreBaseUrl, username) {
  const url = new URL("/auth/passkey/dev/login", `${coreBaseUrl}/`).toString();
  const body = username ? { username } : {};
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      accept: "application/json",
    },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    return null;
  }
  const payload = await response.json();
  const tokens = payload?.tokens ?? {};
  const accessToken = String(tokens.access_token ?? "").trim();
  const refreshToken = String(tokens.refresh_token ?? "").trim();
  if (!accessToken) {
    return null;
  }
  return { agent: payload.agent ?? null, accessToken, refreshToken };
}

export async function POST(event) {
  if (!allowDevIdentityRoutes()) {
    return json({ error: { code: "not_found" } }, { status: 404 });
  }

  const resolved = await resolveWorkspaceSlugFromEvent(event);
  if (resolved.error) {
    return json(resolved.error.payload, { status: resolved.error.status });
  }

  let body;
  try {
    body = await event.request.json();
  } catch {
    return json({ error: { code: "invalid_request" } }, { status: 400 });
  }

  const personaId = String(body?.persona_id ?? "").trim();
  if (!personaId) {
    return json({ error: { code: "invalid_request" } }, { status: 400 });
  }

  const bundle = await readLocalDevIdentityBundle();
  const persona = bundle?.personas?.find((p) => p.persona_id === personaId);
  if (!persona) {
    return json({ error: { code: "unknown_persona" } }, { status: 404 });
  }

  // ── 1. Reuse existing session if it already matches this persona ──────
  try {
    const existingAgent = await loadWorkspaceAuthenticatedAgent({
      event,
      workspaceSlug: resolved.workspaceSlug,
      coreBaseUrl: resolved.coreBaseUrl,
    });
    if (existingAgent?.actor_id === persona.actor_id) {
      return json(
        {
          ok: true,
          agent: existingAgent,
          dev_session_result: "reused",
        },
        { headers: { "cache-control": "no-store" } },
      );
    }
  } catch {
    // No valid existing session — proceed to establish one.
  }

  // ── 2. Try the bundle's refresh token (works on first use) ────────────
  const bundleRefreshToken = String(persona.refresh_token ?? "").trim();
  if (bundleRefreshToken) {
    writeWorkspaceRefreshToken(
      event,
      resolved.workspaceSlug,
      bundleRefreshToken,
    );
    try {
      await refreshWorkspaceAuthSession({
        event,
        workspaceSlug: resolved.workspaceSlug,
        coreBaseUrl: resolved.coreBaseUrl,
      });
      const agent = await loadWorkspaceAuthenticatedAgent({
        event,
        workspaceSlug: resolved.workspaceSlug,
        coreBaseUrl: resolved.coreBaseUrl,
      });
      if (agent) {
        return json(
          {
            ok: true,
            agent,
            dev_session_result: "refreshed",
          },
          { headers: { "cache-control": "no-store" } },
        );
      }
    } catch (error) {
      if (isRetryableWorkspaceAuthSessionError(error)) {
        return json(
          { error: { code: error.code, message: error.message } },
          { status: 503, headers: { "cache-control": "no-store" } },
        );
      }
      // Bundle token consumed — fall through to dev login.
    }
  }

  // ── 3. Fallback: dev login to get fresh tokens (handles rotated tokens)
  const username = String(persona.auth_username ?? "").trim();
  try {
    const loginResult = await devLoginForFreshTokens(
      resolved.coreBaseUrl,
      username,
    );
    if (loginResult) {
      if (loginResult.refreshToken) {
        writeWorkspaceRefreshToken(
          event,
          resolved.workspaceSlug,
          loginResult.refreshToken,
        );
      }
      writeWorkspaceAccessToken(
        event,
        resolved.workspaceSlug,
        loginResult.accessToken,
      );
      return json(
        {
          ok: true,
          agent: loginResult.agent ?? null,
          dev_session_result: "reissued",
        },
        { headers: { "cache-control": "no-store" } },
      );
    }
  } catch {
    // Dev login unavailable.
  }

  return json(
    {
      error: {
        code: "auth_failed",
        message:
          "Could not establish a dev session. The refresh token may be consumed and dev login unavailable. Re-run `make serve` to re-seed.",
      },
    },
    { status: 502, headers: { "cache-control": "no-store" } },
  );
}
