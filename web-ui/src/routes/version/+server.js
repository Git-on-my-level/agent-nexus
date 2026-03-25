import { json } from "@sveltejs/kit";

import { getExpectedCommandRegistryDigest } from "$lib/commandRegistryDigest";
import { EXPECTED_SCHEMA_VERSION } from "$lib/config";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export async function GET({ url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  return json({
    schema_version: EXPECTED_SCHEMA_VERSION,
    command_registry_digest: await getExpectedCommandRegistryDigest(),
  });
}
