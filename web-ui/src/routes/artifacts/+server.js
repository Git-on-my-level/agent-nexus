import { json } from "@sveltejs/kit";

import { listMockArtifacts } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const params = url.searchParams;
  const filters = {
    kind: params.get("kind") ?? undefined,
    thread_id: params.get("thread_id") ?? undefined,
    created_before: params.get("created_before") ?? undefined,
    created_after: params.get("created_after") ?? undefined,
  };

  return json({
    artifacts: listMockArtifacts(filters),
  });
}
