import { json } from "@sveltejs/kit";

import { ackMockInboxItem, createMockEvent } from "$lib/mockCoreData";
import { assertMockModeEnabled, readMockJsonBody } from "$lib/server/mockGuard";

export async function POST({ params, request, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const parsed = await readMockJsonBody(request);
  if (!parsed.ok) {
    return parsed.response;
  }
  const body = parsed.body;

  const subjectRef = String(body?.subject_ref ?? "").trim();
  const inboxItemId = String(params?.inbox_id ?? "").trim();
  if (!body?.actor_id || !inboxItemId || !subjectRef) {
    return json(
      {
        error: "actor_id, inbox_id, and subject_ref are required.",
      },
      { status: 400 },
    );
  }

  const item = ackMockInboxItem({
    subject_ref: subjectRef,
    inbox_item_id: inboxItemId,
  });

  if (!item) {
    return json({ error: "Inbox item not found." }, { status: 404 });
  }

  const eventThreadId = String(item.thread_id ?? "").trim();

  const event = createMockEvent({
    id: `event-${Math.random().toString(36).slice(2, 10)}`,
    ts: new Date().toISOString(),
    type: "inbox_item_acknowledged",
    actor_id: body.actor_id,
    thread_id: eventThreadId,
    refs: [`inbox:${inboxItemId}`, `thread:${eventThreadId}`],
    summary: `Acknowledged inbox item ${inboxItemId}`,
    payload: {
      inbox_item_id: inboxItemId,
    },
    provenance: {
      sources: ["actor_statement:ui"],
    },
  });

  return json({ event });
}
