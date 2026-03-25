import { json } from "@sveltejs/kit";
import { listMockDocuments, createMockDocument } from "$lib/mockCoreData";
import {
  assertMockModeEnabled,
  mockResultToResponse,
  readMockJsonBody,
} from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) return guardResponse;
  const params = url.searchParams;
  const filters = {
    include_tombstoned: params.get("include_tombstoned") === "true",
    thread_id: params.get("thread_id") ?? undefined,
  };
  return json({ documents: listMockDocuments(filters) });
}

export async function POST({ url, request }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) return guardResponse;

  const parsed = await readMockJsonBody(request);
  if (!parsed.ok) {
    return parsed.response;
  }
  const body = parsed.body;

  const { actor_id, document, content, content_type } = body ?? {};

  if (!actor_id) {
    return json({ error: "actor_id is required." }, { status: 400 });
  }

  const result = createMockDocument({
    actor_id,
    document: document ?? {},
    content,
    content_type,
  });

  return mockResultToResponse(result, 201);
}
