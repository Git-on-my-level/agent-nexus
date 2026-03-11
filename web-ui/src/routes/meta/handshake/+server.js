import { json } from "@sveltejs/kit";

import { getExpectedCommandRegistryDigest } from "$lib/commandRegistryDigest";
import { EXPECTED_SCHEMA_VERSION } from "$lib/config";
import { guardMockRoute } from "$lib/server/mockGuard";

const MOCK_CORE_VERSION = "dev-mock";
const MOCK_API_VERSION = "0.2";
const MOCK_MIN_CLI_VERSION = "0.0.0";
const MOCK_RECOMMENDED_CLI_VERSION = "0.0.0";
const MOCK_DOWNLOAD_URL = "https://example.invalid/oar-cli";
const MOCK_INSTANCE_ID = "web-ui-mock-instance";

export async function GET({ url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const commandRegistryDigest = await getExpectedCommandRegistryDigest();
  return json({
    core_version: MOCK_CORE_VERSION,
    api_version: MOCK_API_VERSION,
    schema_version: EXPECTED_SCHEMA_VERSION,
    command_registry_digest: commandRegistryDigest,
    min_cli_version: MOCK_MIN_CLI_VERSION,
    recommended_cli_version: MOCK_RECOMMENDED_CLI_VERSION,
    cli_download_url: MOCK_DOWNLOAD_URL,
    core_instance_id: MOCK_INSTANCE_ID,
  });
}
