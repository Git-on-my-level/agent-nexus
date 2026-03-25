import { json } from "@sveltejs/kit";

import { createMockWorkOrder } from "$lib/mockCoreData";
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

  if (!body?.actor_id || !body?.artifact || !body?.packet) {
    return json(
      { error: "actor_id, artifact, and packet are required." },
      { status: 400 },
    );
  }

  const result = createMockWorkOrder(body);
  if (result.error) {
    return json({ error: result.message }, { status: 400 });
  }

  return json(
    {
      artifact: result.artifact,
      event: result.event,
    },
    { status: 201 },
  );
}
