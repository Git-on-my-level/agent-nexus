import { json } from "@sveltejs/kit";

import { moveMockBoardCard } from "$lib/mockCoreData";
import {
  assertMockModeEnabled,
  mockResultToResponse,
  readMockJsonBody,
} from "$lib/server/mockGuard";

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
  if (!body?.if_board_updated_at) {
    return json({ error: "if_board_updated_at is required." }, { status: 400 });
  }
  if (!body?.column_key) {
    return json({ error: "column_key is required." }, { status: 400 });
  }

  const result = moveMockBoardCard(params.boardId, params.cardId, body);
  return mockResultToResponse(result);
}
