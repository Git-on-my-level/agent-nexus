import { browser } from "$app/environment";
import {
  createAuthTokenProvider,
  getAuthenticatedActorId,
} from "$lib/authSession";
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
    const fetchFn = globalThis.fetch.bind(globalThis);
    browserClient = createOarCoreClient({
      actorIdProvider: () => getAuthenticatedActorId() || getSelectedActorId(),
      lockActorIdProvider: () => Boolean(getAuthenticatedActorId()),
      tokenProvider: createAuthTokenProvider(fetchFn),
      fetchFn,
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
