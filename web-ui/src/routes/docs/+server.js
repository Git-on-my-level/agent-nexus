import { json } from "@sveltejs/kit";
import { listMockDocuments, createMockDocument } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) return guardResponse;
  const params = url.searchParams;
  const filters = {
    include_tombstoned: params.get("include_tombstoned") === "true",
  };
  return json({ documents: listMockDocuments(filters) });
}

export async function POST({ url, request }) {
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

  const { actor_id, document, content, content_type } = body ?? {};

  if (!actor_id) {
    return json(
      { error: { code: "invalid_request", message: "actor_id is required." } },
      { status: 400 },
    );
  }

  const result = createMockDocument({
    actor_id,
    document: document ?? {},
    content,
    content_type,
  });

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

  return json(result, { status: 201 });
}
