import { json } from "@sveltejs/kit";
import { getMockDocumentRevision } from "$lib/mockCoreData";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export function GET({ url, params }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) return guardResponse;
  const revision = getMockDocumentRevision(
    params.documentId,
    params.revisionId,
  );
  if (!revision)
    return json(
      {
        error: {
          code: "not_found",
          message: "revision not found",
          recoverable: false,
          hint: "",
        },
      },
      { status: 404 },
    );
  return json({ revision });
}
