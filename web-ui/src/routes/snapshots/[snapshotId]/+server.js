import { json } from "@sveltejs/kit";

import { getMockCard, getMockThread } from "$lib/mockCoreData";
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

  const card = getMockCard(snapshotId);
  if (card) {
    return json({ snapshot: { ...card, kind: "card" } });
  }

  return json({ error: "Record not found." }, { status: 404 });
}
