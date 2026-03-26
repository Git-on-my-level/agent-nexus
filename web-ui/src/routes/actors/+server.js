import { json } from "@sveltejs/kit";

import { createMockActor, listMockActors } from "$lib/mockCoreData";
import { assertMockModeEnabled, readMockJsonBody } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  return json({ actors: listMockActors() });
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
  const actor = body?.actor;

  if (!actor?.id || !actor?.display_name || !actor?.created_at) {
    return json(
      {
        error: "Invalid actor payload. Expected id, display_name, created_at.",
      },
      { status: 400 },
    );
  }

  const created = createMockActor({
    id: actor.id,
    display_name: actor.display_name,
    tags: actor.tags ?? [],
    created_at: actor.created_at,
  });

  return json({ actor: created }, { status: 201 });
}
