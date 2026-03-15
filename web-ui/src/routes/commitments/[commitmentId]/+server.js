import { json } from "@sveltejs/kit";

import { getMockCommitment, updateMockCommitment } from "$lib/mockCoreData";
import { guardMockRoute, mockResultToResponse } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const commitment = getMockCommitment(params.commitmentId);
  if (!commitment) {
    return json({ error: "Commitment not found." }, { status: 404 });
  }

  return json({ commitment });
}

export async function PATCH({ params, request, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const body = await request.json();

  if (!body?.actor_id || !body?.patch) {
    return json({ error: "actor_id and patch are required." }, { status: 400 });
  }

  const result = updateMockCommitment({
    actor_id: body.actor_id,
    commitment_id: params.commitmentId,
    patch: body.patch,
    refs: body.refs ?? [],
    if_updated_at: body.if_updated_at,
  });

  if (result.error === "invalid_transition") {
    return json({ error: result.message }, { status: 400 });
  }

  return mockResultToResponse(result);
}
