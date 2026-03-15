import { json } from "@sveltejs/kit";

import { listMockBoards, createMockBoard } from "$lib/mockCoreData";
import { guardMockRoute, mockResultToResponse } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const params = url.searchParams;
  const filters = {
    status: params.get("status") ?? undefined,
    label: params.getAll("label"),
    owner: params.getAll("owner"),
  };

  return json({
    boards: listMockBoards(filters),
  });
}

export async function POST({ request, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const body = await request.json();

  if (!body?.actor_id) {
    return json({ error: "actor_id is required." }, { status: 400 });
  }

  if (!body?.board?.title || !body?.board?.primary_thread_id) {
    return json(
      { error: "board.title and board.primary_thread_id are required." },
      { status: 400 },
    );
  }

  const created = createMockBoard(body);
  return mockResultToResponse(created, 201);
}
