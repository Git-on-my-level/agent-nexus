import {
  createAnxCoreClient,
  verifyCoreSchemaVersion,
} from "$lib/anxCoreClient";
import { WORKSPACE_HEADER_CONSTANTS } from "$lib/compat/workspaceCompat";
import { WORKSPACE_HEADER, workspaceCompositeKey } from "$lib/workspacePaths";

const schemaCheckPromises = new Map();

export async function load({ fetch, data, url }) {
  const workspaceSlug = data.workspace?.slug ?? "";
  const organizationSlug = data.workspace?.organizationSlug ?? "";
  if (!workspaceSlug) {
    return;
  }

  if (url.searchParams.get("qa") === "1") {
    return;
  }

  const coreBaseUrl = String(data.workspace?.coreBaseUrl ?? "").trim();
  if (!coreBaseUrl) {
    return;
  }

  const cacheKey = workspaceCompositeKey(organizationSlug, workspaceSlug);
  if (!schemaCheckPromises.has(cacheKey)) {
    const client = createAnxCoreClient({
      baseUrl: coreBaseUrl,
      fetchFn: fetch,
      requestContextHeadersProvider: () => ({
        [WORKSPACE_HEADER]: workspaceSlug,
        [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: organizationSlug,
      }),
    });
    const promise = verifyCoreSchemaVersion(client).catch((error) => {
      schemaCheckPromises.delete(cacheKey);
      throw error;
    });
    schemaCheckPromises.set(cacheKey, promise);
  }

  await schemaCheckPromises.get(cacheKey);
}
