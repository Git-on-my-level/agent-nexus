import { json } from "@sveltejs/kit";
import { getMockDocument, updateMockDocument } from "$lib/mockCoreData";
import {
  assertMockModeEnabled,
  mockResultToResponse,
  readMockJsonBody,
} from "$lib/server/mockGuard";

export function GET({ url, params }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) return guardResponse;
  const result = getMockDocument(params.documentId);
  if (!result) return json({ error: "document not found" }, { status: 404 });
  return json(result);
}

export async function PATCH({ url, params, request }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) return guardResponse;

  const parsed = await readMockJsonBody(request);
  if (!parsed.ok) {
    return parsed.response;
  }
  const body = parsed.body;

  const { actor_id, content, content_type, if_base_revision, document } =
    body ?? {};

  if (!actor_id) {
    return json({ error: "actor_id is required." }, { status: 400 });
  }

  const result = updateMockDocument({
    actor_id,
    document_id: params.documentId,
    content,
    content_type,
    if_base_revision,
    document: document ?? {},
  });

  return mockResultToResponse(result);
}
