import { json } from "@sveltejs/kit";
import { getMockDocument, updateMockDocument } from "$lib/mockCoreData";
import { guardMockRoute, mockResultToResponse } from "$lib/server/mockGuard";

export function GET({ url, params }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) return guardResponse;
  const result = getMockDocument(params.documentId);
  if (!result) return json({ error: "document not found" }, { status: 404 });
  return json(result);
}

export async function PATCH({ url, params, request }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) return guardResponse;

  let body;
  try {
    body = await request.json();
  } catch {
    return json({ error: "Invalid JSON body." }, { status: 400 });
  }

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
