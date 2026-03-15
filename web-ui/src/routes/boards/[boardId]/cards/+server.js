import { json } from "@sveltejs/kit";

import { listMockBoardCards, createMockBoardCard } from "$lib/mockCoreData";
import { guardMockRoute, mockResultToResponse } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const cards = listMockBoardCards(params.boardId);
  return json({ board_id: params.boardId, cards });
}

export async function POST({ params, request, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const body = await request.json();

  if (!body?.actor_id) {
    return json({ error: "actor_id is required." }, { status: 400 });
  }

  if (!body?.thread_id) {
    return json({ error: "thread_id is required." }, { status: 400 });
  }

  const result = createMockBoardCard(params.boardId, body);
  return mockResultToResponse(result, 201);
}
