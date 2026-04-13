import {
  createOarCoreClient,
  verifyCoreSchemaVersion,
} from "$lib/oarCoreClient";
import { WORKSPACE_HEADER } from "$lib/workspacePaths";

const schemaCheckPromises = new Map();

export async function load({ fetch, data }) {
  const workspaceSlug = data.workspace?.slug ?? "";
  if (!workspaceSlug) {
    return;
  }

  const coreBaseUrl = String(data.workspace?.coreBaseUrl ?? "").trim();
  if (!coreBaseUrl) {
    return;
  }

  if (!schemaCheckPromises.has(workspaceSlug)) {
    const client = createOarCoreClient({
      fetchFn: fetch,
      requestContextHeadersProvider: () => ({
        [WORKSPACE_HEADER]: workspaceSlug,
      }),
    });
    const promise = verifyCoreSchemaVersion(client).catch((error) => {
      schemaCheckPromises.delete(workspaceSlug);
      throw error;
    });
    schemaCheckPromises.set(workspaceSlug, promise);
  }

  await schemaCheckPromises.get(workspaceSlug);
}
