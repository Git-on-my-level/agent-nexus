import { json } from "@sveltejs/kit";
import { getMockDocument, updateMockDocument } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ url, params }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) return guardResponse;
  const result = getMockDocument(params.documentId);
  if (!result)
    return json(
      {
        error: {
          code: "not_found",
          message: "document not found",
          recoverable: false,
          hint: "",
        },
      },
      { status: 404 },
    );
  return json(result);
}

export async function PATCH({ url, params, request }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) return guardResponse;

  let body;
  try {
    body = await request.json();
  } catch {
    return json(
      { error: { code: "invalid_json", message: "Invalid JSON body." } },
      { status: 400 },
    );
  }

  const { actor_id, content, content_type, if_base_revision, document } =
    body ?? {};

  if (!actor_id) {
    return json(
      { error: { code: "invalid_request", message: "actor_id is required." } },
      { status: 400 },
    );
  }

  const result = updateMockDocument({
    actor_id,
    document_id: params.documentId,
    content,
    content_type,
    if_base_revision,
    document: document ?? {},
  });

  if (result.error === "not_found") {
    return json(
      {
        error: {
          code: "not_found",
          message: result.message,
          recoverable: false,
          hint: "",
        },
      },
      { status: 404 },
    );
  }

  if (result.error === "validation") {
    return json(
      { error: { code: "invalid_request", message: result.message } },
      { status: 400 },
    );
  }

  if (result.error === "conflict") {
    return json(
      { error: { code: "conflict", message: result.message } },
      { status: 409 },
    );
  }

  return json(result);
}
