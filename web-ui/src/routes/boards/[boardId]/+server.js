import { json } from "@sveltejs/kit";

import { getMockBoard, updateMockBoard } from "$lib/mockCoreData.js";
import { guardMockRoute } from "$lib/server/mockGuard";

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
  if (updated?.error === "validation") {
    return json({ error: updated.message }, { status: 400 });
  }
  if (updated?.error === "conflict") {
    return json(updated, { status: 409 });
  }
  if (updated?.error === "not_found") {
    return json({ error: updated.message }, { status: 404 });
  }

  return json(updated);
}
