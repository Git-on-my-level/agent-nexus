import { json } from "@sveltejs/kit";

import {
  createMockThread,
  getMockTopic,
  listMockTopics,
} from "$lib/mockCoreData";
import { assertMockModeEnabled, readMockJsonBody } from "$lib/server/mockGuard";
import {
  mapTopicStatusToThreadStatus,
  mapTopicTypeToThreadType,
} from "$lib/server/mockTopicMappers";

function truthyParam(value) {
  return value === true || String(value) === "true";
}

export function GET({ url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const type = url.searchParams.get("type") ?? "";
  const status = url.searchParams.get("status") ?? "";
  const q = url.searchParams.get("q") ?? "";
  const archivedOnly = truthyParam(url.searchParams.get("archived_only"));
  const trashedOnly = truthyParam(url.searchParams.get("trashed_only"));
  const limitRaw = url.searchParams.get("limit");
  const limit =
    limitRaw != null && limitRaw !== ""
      ? Math.min(1000, Math.max(1, Number.parseInt(limitRaw, 10) || 0))
      : null;

  if (archivedOnly || trashedOnly) {
    return json({ topics: [] });
  }

  let topics = listMockTopics({});
  if (type) {
    topics = topics.filter((t) => String(t.type) === type);
  }
  if (status) {
    topics = topics.filter((t) => String(t.status) === status);
  }
  if (q) {
    const qq = q.trim().toLowerCase();
    topics = topics.filter(
      (t) =>
        String(t.id).toLowerCase().includes(qq) ||
        String(t.title ?? "")
          .toLowerCase()
          .includes(qq),
    );
  }

  let next_cursor;
  if (limit != null && Number.isFinite(limit) && topics.length > limit) {
    const page = topics.slice(0, limit);
    next_cursor = `mock:${page.at(-1)?.id ?? ""}`;
    topics = page;
  }

  const body = { topics };
  if (next_cursor) {
    body.next_cursor = next_cursor;
  }
  return json(body);
}

export async function POST({ request, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const parsed = await readMockJsonBody(request);
  if (!parsed.ok) {
    return parsed.response;
  }
  const body = parsed.body;

  if (!body?.actor_id) {
    return json({ error: "actor_id is required." }, { status: 400 });
  }
  const topicIn = body?.topic;
  if (!topicIn || typeof topicIn !== "object") {
    return json({ error: "topic envelope is required." }, { status: 400 });
  }

  const title = String(topicIn.title ?? "").trim();
  const summary = String(topicIn.summary ?? "").trim();
  if (!title) {
    return json({ error: "topic.title is required." }, { status: 400 });
  }

  const created = createMockThread({
    actor_id: body.actor_id,
    thread: {
      type: mapTopicTypeToThreadType(topicIn.type),
      title,
      status: mapTopicStatusToThreadStatus(topicIn.status),
      current_summary: summary || "No summary provided.",
      cadence: "weekly",
      priority: "p2",
      tags: [],
      next_actions: [],
      open_cards: [],
      key_artifacts: [],
    },
  });

  const topic = getMockTopic(created.id);
  if (!topic) {
    return json({ error: "Topic create failed." }, { status: 500 });
  }

  return json({ topic }, { status: 201 });
}
