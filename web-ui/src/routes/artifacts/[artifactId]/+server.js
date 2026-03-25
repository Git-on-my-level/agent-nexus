import { json } from "@sveltejs/kit";

import { getMockArtifact } from "$lib/mockCoreData";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const artifact = getMockArtifact(params.artifactId);
  if (!artifact) {
    return json({ error: "Artifact not found." }, { status: 404 });
  }

  return json({ artifact });
}
