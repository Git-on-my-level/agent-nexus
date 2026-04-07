import { json } from "@sveltejs/kit";

import { getMockTopic, updateMockThread } from "$lib/mockCoreData";
import {
  assertMockModeEnabled,
  mockResultToResponse,
  readMockJsonBody,
} from "$lib/server/mockGuard";
import { mapTopicPatchToThreadPatch } from "$lib/server/mockTopicMappers";

export function GET({ params, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const topic = getMockTopic(params.topicId);

  if (!topic) {
    return json({ error: "Topic not found." }, { status: 404 });
  }

  return json({ topic });
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

  const topicRow = getMockTopic(params.topicId);
  if (!topicRow) {
    return json({ error: "Topic not found." }, { status: 404 });
  }

  const threadPatch = mapTopicPatchToThreadPatch(body.patch);
  const result = updateMockThread({
    actor_id: body.actor_id,
    thread_id: topicRow.thread_id,
    patch: threadPatch,
    if_updated_at: body.if_updated_at,
  });

  if (result?.error) {
    return mockResultToResponse(result);
  }

  const nextTopic = getMockTopic(params.topicId);
  if (!nextTopic) {
    return json({ error: "Topic not found after update." }, { status: 404 });
  }

  return json({ topic: nextTopic });
}
