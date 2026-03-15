import { json } from "@sveltejs/kit";

import { removeMockBoardCard } from "$lib/mockCoreData";
import { guardMockRoute, mockResultToResponse } from "$lib/server/mockGuard";

export async function POST({ params, request, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const body = await request.json();

  if (!body?.actor_id) {
    return json({ error: "actor_id is required." }, { status: 400 });
  }
  if (!body?.if_board_updated_at) {
    return json({ error: "if_board_updated_at is required." }, { status: 400 });
  }

  const result = removeMockBoardCard(params.boardId, params.cardId, body);
  return mockResultToResponse(result);
}
