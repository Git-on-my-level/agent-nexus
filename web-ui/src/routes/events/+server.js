import { json } from "@sveltejs/kit";

import { validateEventCreatePayload } from "$lib/eventValidation";
import { createMockEvent } from "$lib/mockCoreData";
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
  const validationError = validateEventCreatePayload(body);
  if (validationError) {
    return json({ error: validationError }, { status: 400 });
  }

  const eventInput = body.event;
  const event = createMockEvent({
    id: `event-${Math.random().toString(36).slice(2, 10)}`,
    ts: new Date().toISOString(),
    actor_id: body.actor_id,
    ...eventInput,
  });

  return json({ event }, { status: 201 });
}
