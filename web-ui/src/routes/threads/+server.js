import { json } from "@sveltejs/kit";

import { createMockThread, listMockThreads } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const params = url.searchParams;
  const filters = {
    status: params.get("status") ?? undefined,
    priority: params.get("priority") ?? undefined,
    cadence: params.get("cadence") ?? undefined,
    stale: params.get("stale") ?? undefined,
    tag: params.getAll("tag"),
  };

  return json({
    threads: listMockThreads(filters),
  });
}

export async function POST({ request, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const body = await request.json();

  if (!body?.actor_id || !body?.thread?.title) {
    return json(
      { error: "actor_id and thread.title are required." },
      { status: 400 },
    );
  }

  const created = createMockThread(body);
  return json({ thread: created });
}
