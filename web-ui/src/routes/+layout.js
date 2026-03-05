import {
  createOarCoreClient,
  verifyCoreSchemaVersion,
} from "$lib/oarCoreClient";

let schemaCheckPromise;

export async function load({ fetch }) {
  if (!schemaCheckPromise) {
    const client = createOarCoreClient({ fetchFn: fetch });
    schemaCheckPromise = verifyCoreSchemaVersion(client).catch((error) => {
      schemaCheckPromise = undefined;
      throw error;
    });
  }

  await schemaCheckPromise;
}
