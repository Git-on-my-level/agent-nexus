import { json } from "@sveltejs/kit";

import { getMockThreadWorkspace } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const maxEventsRaw = url.searchParams.get("max_events");
  const maxEvents =
    maxEventsRaw == null ? undefined : Number.parseInt(maxEventsRaw, 10);
  const workspace = getMockThreadWorkspace(params.threadId, {
    max_events: Number.isFinite(maxEvents) ? maxEvents : undefined,
    include_artifact_content:
      url.searchParams.get("include_artifact_content") === "true",
  });

  if (!workspace) {
    return json({ error: "Thread not found." }, { status: 404 });
  }

  return json(workspace);
}
