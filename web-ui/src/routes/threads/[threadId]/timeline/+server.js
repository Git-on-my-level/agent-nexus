import { json } from "@sveltejs/kit";

import { getMockThread, listMockTimelineEvents } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const thread = getMockThread(params.threadId);
  if (!thread) {
    return json({ error: "Thread not found." }, { status: 404 });
  }

  return json({
    events: listMockTimelineEvents(params.threadId),
    snapshots: [],
    artifacts: [],
  });
}
