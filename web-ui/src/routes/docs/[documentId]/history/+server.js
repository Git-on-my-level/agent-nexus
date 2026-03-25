import { json } from "@sveltejs/kit";
import { getMockDocumentHistory } from "$lib/mockCoreData";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export function GET({ url, params }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) return guardResponse;
  const revisions = getMockDocumentHistory(params.documentId);
  return json({ document_id: params.documentId, revisions });
}
