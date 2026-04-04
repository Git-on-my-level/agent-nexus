import { json } from "@sveltejs/kit";

import { artifactForApiResponse, listMockArtifacts } from "$lib/mockCoreData";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const params = url.searchParams;
  const filters = {
    kind: params.get("kind") ?? undefined,
    thread_id: params.get("thread_id") ?? undefined,
    created_before: params.get("created_before") ?? undefined,
    created_after: params.get("created_after") ?? undefined,
    include_tombstoned: params.get("include_tombstoned") ?? undefined,
    tombstoned_only: params.get("tombstoned_only") ?? undefined,
  };

  return json({
    artifacts: listMockArtifacts(filters).map(artifactForApiResponse),
  });
}
