import { json } from "@sveltejs/kit";

import { purgeMockBoardCardByCardId } from "$lib/mockCoreData";
import {
  assertMockModeEnabled,
  mockResultToResponse,
  readMockJsonBody,
} from "$lib/server/mockGuard";

/** Mock route: POST /cards/{card_id}/purge */
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
  if (!body?.actor_id) {
    return json({ error: "actor_id is required." }, { status: 400 });
  }

  const result = purgeMockBoardCardByCardId(params.card_id, body);
  return mockResultToResponse(result);
}
