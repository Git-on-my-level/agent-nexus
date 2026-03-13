import { json } from "@sveltejs/kit";

import { moveMockBoardCard } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

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
  if (!body?.column_key) {
    return json({ error: "column_key is required." }, { status: 400 });
  }

  const result = moveMockBoardCard(params.boardId, params.cardId, body);
  if (result?.error === "conflict") {
    return json(result, { status: 409 });
  }
  if (result?.error === "not_found") {
    return json({ error: result.message }, { status: 404 });
  }
  if (result?.error === "validation") {
    return json({ error: result.message }, { status: 400 });
  }

  return json(result);
}
