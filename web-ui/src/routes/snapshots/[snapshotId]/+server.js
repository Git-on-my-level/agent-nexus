import { json } from "@sveltejs/kit";

import { getMockCommitment, getMockThread } from "$lib/mockCoreData";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const snapshotId = params.snapshotId;

  const thread = getMockThread(snapshotId);
  if (thread) {
    return json({ snapshot: thread });
  }

  const commitment = getMockCommitment(snapshotId);
  if (commitment) {
    return json({ snapshot: commitment });
  }

  return json({ error: "Snapshot not found." }, { status: 404 });
}
