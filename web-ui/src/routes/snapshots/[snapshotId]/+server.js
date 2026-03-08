import { json } from "@sveltejs/kit";

import { getMockThread, commitments } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const snapshotId = params.snapshotId;

  const thread = getMockThread(snapshotId);
  if (thread) {
    return json({ snapshot: thread });
  }

  const commitment = commitments.find((c) => c.id === snapshotId) ?? null;
  if (commitment) {
    return json({ snapshot: commitment });
  }

  return json({ error: "Snapshot not found." }, { status: 404 });
}
