import { json } from "@sveltejs/kit";

import { EXPECTED_SCHEMA_VERSION } from "$lib/config";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  return json({ schema_version: EXPECTED_SCHEMA_VERSION });
}
