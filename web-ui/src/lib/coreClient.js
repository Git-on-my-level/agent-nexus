import { browser } from "$app/environment";
import {
  getAuthenticatedActorId,
  getAuthenticatedAgent,
} from "$lib/authSession";
import { getSelectedActorId } from "$lib/actorSession";
import { createAnxCoreClient } from "$lib/anxCoreClient";
import { buildCoreRequestContextHeaders } from "$lib/coreClientRequestHeaders";
import {
  getCurrentOrganizationSlug,
  getCurrentWorkspaceSlug,
} from "$lib/workspaceContext";
import { APP_BASE_PATH } from "$lib/workspacePaths";

let browserClient;

/**
 * Options passed to {@link createAnxCoreClient} for the browser shell (tests
 * can assert actor/lock wiring without instantiating the full proxy).
 */
export function getBrowserCoreClientOptions() {
  return {
    actorIdProvider: () => getAuthenticatedActorId() || getSelectedActorId(),
    lockActorIdProvider: () => Boolean(getAuthenticatedAgent()?.agent_id),
    requestContextHeadersProvider: () =>
      buildCoreRequestContextHeaders({
        storeOrg: getCurrentOrganizationSlug(),
        storeWorkspace: getCurrentWorkspaceSlug(),
        pathname: globalThis.location?.pathname ?? "/",
        basePath: APP_BASE_PATH,
      }),
  };
}

function resolveBrowserClient() {
  if (!browser) {
    throw new Error(
      "coreClient cannot run during SSR. Use onMount or a load-scoped client created with createAnxCoreClient({ fetchFn: fetch }).",
    );
  }

  if (!browserClient) {
    const fetchFn = globalThis.fetch.bind(globalThis);
    browserClient = createAnxCoreClient({
      ...getBrowserCoreClientOptions(),
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
