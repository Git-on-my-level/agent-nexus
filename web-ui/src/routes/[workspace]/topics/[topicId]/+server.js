import { json } from "@sveltejs/kit";

import { getMockThread, updateMockThread } from "$lib/mockCoreData";
import {
  assertMockModeEnabled,
  mockResultToResponse,
  readMockJsonBody,
} from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const topic = getMockThread(params.topicId);

  if (!topic) {
    return json({ error: "Topic not found." }, { status: 404 });
  }

  return json({ thread: topic });
}

export async function PATCH({ params, request, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const parsed = await readMockJsonBody(request);
  if (!parsed.ok) {
    return parsed.response;
  }
  const body = parsed.body;

  if (!body?.actor_id || !body?.patch) {
    return json({ error: "actor_id and patch are required." }, { status: 400 });
  }

  const result = updateMockThread({
    actor_id: body.actor_id,
    thread_id: params.topicId,
    patch: body.patch,
    if_updated_at: body.if_updated_at,
  });

  return mockResultToResponse(result);
}
