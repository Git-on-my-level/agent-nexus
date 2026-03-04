import { json } from "@sveltejs/kit";

import { listMockArtifacts } from "$lib/mockCoreData";

export function GET({ url }) {
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
