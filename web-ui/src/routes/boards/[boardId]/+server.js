import { json } from "@sveltejs/kit";

import { getMockBoard, updateMockBoard } from "$lib/mockCoreData.js";
import { guardMockRoute, mockResultToResponse } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const board = getMockBoard(params.boardId);
  if (!board) {
    return json({ error: "Board not found" }, { status: 404 });
  }

  return json({ board });
}

export async function PATCH({ params, request, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const body = await request.json();

  if (!body?.actor_id) {
    return json({ error: "actor_id is required." }, { status: 400 });
  }

  if (!body?.if_updated_at) {
    return json({ error: "if_updated_at is required." }, { status: 400 });
  }

  const updated = updateMockBoard(params.boardId, body);
  return mockResultToResponse(updated);
}
