import { json } from "@sveltejs/kit";
import { dev } from "$app/environment";
import {
  loadWorkspaceAuthenticatedAgent,
  writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken,
} from "$lib/server/authSession.js";
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver.js";
import {
  LAST_WORKSPACE_COOKIE,
  lastWorkspaceCookieValue,
} from "$lib/server/workspaceRedirect.js";

// Explicit selection only. GET/data preloads never establish a session.
export async function POST(event) {
  const headers = { "cache-control": "private, no-store" };
  if (
    event.request.headers.get("origin") !== event.url.origin ||
    ["cross-site", "same-site"].includes(
      event.request.headers.get("sec-fetch-site"),
    )
  )
    return json({ error: "origin_required" }, { status: 403, headers });
  if (
    !event.request.headers.get("content-type")?.startsWith("application/json")
  )
    return json({ error: "json_required" }, { status: 415, headers });
  const adapter = event.locals.sessionAdapter;
  if (!adapter)
    return json(
      { error: "session_adapter_unavailable" },
      { status: 503, headers },
    );
  let input;
  try {
    input = await event.request.json();
  } catch {
    return json({ error: "invalid_json" }, { status: 400, headers });
  }
  const { organizationSlug, workspaceSlug } = input ?? {};
  if (typeof organizationSlug !== "string" || typeof workspaceSlug !== "string")
    return json({ error: "workspace_required" }, { status: 400, headers });
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug,
    workspaceSlug,
  });
  if (resolved.error)
    return json(resolved.error.payload, {
      status: resolved.error.status,
      headers,
    });
  try {
    const target = adapter.sessionTarget(resolved.workspace);
    let agent = await loadWorkspaceAuthenticatedAgent({
      event,
      organizationSlug,
      workspaceSlug,
      ...target,
    });
    if (!agent) {
      const session = await adapter.establishSession(resolved.workspace);
      if (
        !session.tokens?.access_token ||
        !session.tokens?.refresh_token ||
        !session.agent?.agent_id
      )
        throw new Error("Incomplete session from adapter");
      writeWorkspaceAccessToken(
        event,
        organizationSlug,
        workspaceSlug,
        session.tokens.access_token,
      );
      writeWorkspaceRefreshToken(
        event,
        organizationSlug,
        workspaceSlug,
        session.tokens.refresh_token,
      );
      agent = session.agent;
    }
    event.cookies.set(
      LAST_WORKSPACE_COOKIE,
      lastWorkspaceCookieValue(organizationSlug, workspaceSlug),
      {
        path: "/",
        httpOnly: true,
        sameSite: "lax",
        secure: !dev || event.url.protocol === "https:",
        maxAge: 60 * 60 * 24 * 180,
      },
    );
    return json({ agent }, { headers });
  } catch (failure) {
    return json(
      { error: "session_establishment_failed" },
      { status: failure.status || 502, headers },
    );
  }
}
