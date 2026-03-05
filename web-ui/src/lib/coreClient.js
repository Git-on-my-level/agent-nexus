import { browser } from "$app/environment";
import { getSelectedActorId } from "$lib/actorSession";
import { createOarCoreClient } from "$lib/oarCoreClient";

let browserClient;

function resolveBrowserClient() {
  if (!browser) {
    throw new Error(
      "coreClient cannot run during SSR. Use onMount or a load-scoped client created with createOarCoreClient({ fetchFn: fetch }).",
    );
  }

  if (!browserClient) {
    browserClient = createOarCoreClient({
      actorIdProvider: getSelectedActorId,
      fetchFn: globalThis.fetch.bind(globalThis),
    });
  }

  return browserClient;
}

export const coreClient = new Proxy(
  {},
  {
    get(_target, property) {
      const client = resolveBrowserClient();
      const value = client[property];

      return typeof value === "function" ? value.bind(client) : value;
    },
  },
);
