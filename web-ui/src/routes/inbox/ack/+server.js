import { json } from "@sveltejs/kit";

import { ackMockInboxItem, createMockEvent } from "$lib/mockCoreData";
import { assertMockModeEnabled, readMockJsonBody } from "$lib/server/mockGuard";

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

  if (!body?.actor_id || !body?.thread_id || !body?.inbox_item_id) {
    return json(
      { error: "actor_id, thread_id, and inbox_item_id are required." },
      { status: 400 },
    );
  }

  const item = ackMockInboxItem(body);

  if (!item) {
    return json({ error: "Inbox item not found." }, { status: 404 });
  }

  const event = createMockEvent({
    id: `event-${Math.random().toString(36).slice(2, 10)}`,
    ts: new Date().toISOString(),
    type: "inbox_item_acknowledged",
    actor_id: body.actor_id,
    thread_id: body.thread_id,
    refs: [`inbox:${body.inbox_item_id}`, `thread:${body.thread_id}`],
    summary: `Acknowledged inbox item ${body.inbox_item_id}`,
    payload: {
      inbox_item_id: body.inbox_item_id,
    },
    provenance: {
      sources: ["actor_statement:ui"],
    },
  });

  return json({ event });
}
