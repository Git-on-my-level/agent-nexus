import { json } from "@sveltejs/kit";

import { getMockArtifact } from "$lib/mockCoreData";

export function GET({ params }) {
  const artifact = getMockArtifact(params.artifactId);
  if (!artifact) {
    return json({ error: "Artifact not found." }, { status: 404 });
  }

  return json({ artifact });
}
