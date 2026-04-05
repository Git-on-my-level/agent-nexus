import { json } from "@sveltejs/kit";

import { getMockThread, listMockTimelineEvents } from "$lib/mockCoreData";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const topic = getMockThread(params.topicId);
  if (!topic) {
    return json({ error: "Topic not found." }, { status: 404 });
  }

  return json({
    events: listMockTimelineEvents(params.topicId),
  });
}
