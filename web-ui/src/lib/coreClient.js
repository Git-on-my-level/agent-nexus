import { browser } from "$app/environment";
import {
  getAuthenticatedActorId,
  getAuthenticatedAgent,
} from "$lib/authSession";
import { getSelectedActorId } from "$lib/actorSession";
import { createAnxCoreClient } from "$lib/anxCoreClient";
import { buildCoreRequestContextHeaders } from "$lib/coreClientRequestHeaders";
import {
  getCurrentCoreBaseUrl,
  getCurrentOrganizationSlug,
  getCurrentWorkspaceSlug,
} from "$lib/workspaceContext";
import { APP_BASE_PATH } from "$lib/workspacePaths";

let browserClient;
let browserClientBaseUrl = null;

/**
 * Options passed to {@link createAnxCoreClient} for the browser shell (tests
 * can assert actor/lock wiring without instantiating the full proxy).
 */
export function getBrowserCoreClientOptions() {
  return {
    baseUrl: getCurrentCoreBaseUrl(),
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

  const fetchFn = globalThis.fetch.bind(globalThis);

  if (!browserClient) {
    const options = getBrowserCoreClientOptions();
    browserClient = createAnxCoreClient({
      ...options,
      fetchFn,
    });
    browserClientBaseUrl = String(options.baseUrl ?? "").trim();
  } else {
    const nextBaseUrl = getCurrentCoreBaseUrl();
    if (String(nextBaseUrl ?? "").trim() !== browserClientBaseUrl) {
      const options = getBrowserCoreClientOptions();
      browserClient = createAnxCoreClient({
        ...options,
        fetchFn,
      });
      browserClientBaseUrl = String(options.baseUrl ?? "").trim();
    }
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
