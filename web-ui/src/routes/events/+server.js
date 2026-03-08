import { json } from "@sveltejs/kit";

import { createMockEvent } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";
import { validateEventRefRule } from "$lib/eventRefRules.js";

export async function POST({ request, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const body = await request.json();
  const eventInput = body?.event;

  if (!body?.actor_id) {
    return json({ error: "actor_id is required" }, { status: 400 });
  }

  if (!eventInput?.type || !eventInput?.summary) {
    return json(
      { error: "event.type and event.summary are required" },
      { status: 400 },
    );
  }

  const refValidation = validateEventRefRule(
    eventInput.type,
    eventInput.refs ?? [],
    eventInput.payload ?? {},
  );
  if (!refValidation.valid) {
    return json({ error: refValidation.error }, { status: 400 });
  }

  const event = createMockEvent({
    id: `event-${Math.random().toString(36).slice(2, 10)}`,
    ts: new Date().toISOString(),
    actor_id: body.actor_id,
    ...eventInput,
    refs: eventInput.refs ?? [],
    provenance: eventInput.provenance ?? { sources: ["actor_statement:ui"] },
  });

  return json({ event });
}
