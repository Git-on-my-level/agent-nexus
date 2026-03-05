import { json } from "@sveltejs/kit";

import { getMockArtifactContent } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const result = getMockArtifactContent(params.artifactId);
  if (!result) {
    return json({ error: "Artifact not found." }, { status: 404 });
  }

  if (result.contentType === "application/json") {
    return json(result.content);
  }

  return new Response(String(result.content ?? ""), {
    status: 200,
    headers: {
      "content-type": result.contentType,
    },
  });
}
