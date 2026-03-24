import { json } from "@sveltejs/kit";

import {
  clearWorkspaceAuthSession,
  loadWorkspaceAuthenticatedAgent,
  resolveWorkspaceSlugFromEvent,
} from "$lib/server/authSession";

export async function GET(event) {
  const resolved = await resolveWorkspaceSlugFromEvent(event);
  if (resolved.error) {
    return json(resolved.error.payload, { status: resolved.error.status });
  }

  try {
    const agent = await loadWorkspaceAuthenticatedAgent({
      event,
      workspaceSlug: resolved.workspaceSlug,
      coreBaseUrl: resolved.coreBaseUrl,
    });

    return json(
      {
        authenticated: Boolean(agent?.agent_id),
        agent: agent ?? null,
      },
      {
        headers: {
          "cache-control": "no-store",
        },
      },
    );
  } catch (error) {
    if (error?.status === 401 || error?.status === 403) {
      clearWorkspaceAuthSession(event, resolved.workspaceSlug);
    }
    return json(
      {
        authenticated: false,
        agent: null,
      },
      {
        headers: {
          "cache-control": "no-store",
        },
        status: error?.status === 401 || error?.status === 403 ? 200 : 503,
      },
    );
  }
}

export async function DELETE(event) {
  const resolved = await resolveWorkspaceSlugFromEvent(event);
  if (resolved.error) {
    return json(resolved.error.payload, { status: resolved.error.status });
  }

  clearWorkspaceAuthSession(event, resolved.workspaceSlug);

  return json(
    {
      ok: true,
    },
    {
      headers: {
        "cache-control": "no-store",
      },
    },
  );
}

export const POST = DELETE;
