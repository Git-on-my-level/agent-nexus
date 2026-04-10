import { json } from "@sveltejs/kit";

import { env as privateEnv } from "$env/dynamic/private";
import { readLocalDevIdentityBundle } from "$lib/server/devIdentityBundle.js";
import {
  isRetryableWorkspaceAuthSessionError,
  loadWorkspaceAuthenticatedAgent,
  refreshWorkspaceAuthSession,
  resolveWorkspaceSlugFromEvent,
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
  const refreshToken = String(persona?.refresh_token ?? "").trim();
  if (!refreshToken) {
    return json({ error: { code: "unknown_persona" } }, { status: 404 });
  }

  writeWorkspaceRefreshToken(event, resolved.workspaceSlug, refreshToken);

  try {
    await refreshWorkspaceAuthSession({
      event,
      workspaceSlug: resolved.workspaceSlug,
      coreBaseUrl: resolved.coreBaseUrl,
    });
  } catch (error) {
    if (isRetryableWorkspaceAuthSessionError(error)) {
      return json(
        {
          error: {
            code: error.code,
            message: error.message,
          },
        },
        { status: 503, headers: { "cache-control": "no-store" } },
      );
    }
    return json(
      {
        error: {
          code: "auth_failed",
          message: error instanceof Error ? error.message : String(error),
        },
      },
      { status: 502, headers: { "cache-control": "no-store" } },
    );
  }

  try {
    const agent = await loadWorkspaceAuthenticatedAgent({
      event,
      workspaceSlug: resolved.workspaceSlug,
      coreBaseUrl: resolved.coreBaseUrl,
    });
    return json(
      { ok: true, agent: agent ?? null },
      { headers: { "cache-control": "no-store" } },
    );
  } catch (error) {
    return json(
      {
        error: {
          code: "session_load_failed",
          message: error instanceof Error ? error.message : String(error),
        },
      },
      { status: 502, headers: { "cache-control": "no-store" } },
    );
  }
}
