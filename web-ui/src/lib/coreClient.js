import { createSharedReadFetch } from "$lib/sharedReadFetch.js";
import { getSessionEpoch } from "$lib/authSession.js";

// A principal/workspace change retires the in-flight sharing namespace even
// when the reader later returns to the same workspace and identity.
const sharedReadFetch = createSharedReadFetch({
  scope: () => [
    getSessionEpoch(),
    getAuthenticatedActorId() || getSelectedActorId(),
  ],
});
const browserReadFetch = (url, init) =>
  sharedReadFetch(globalThis.fetch.bind(globalThis), url, init);

import { browser } from "$app/environment";
import {
  getAuthenticatedActorId,
  getAuthenticatedAgent,
} from "$lib/authSession";
import { getSelectedActorId } from "$lib/actorSession";
import { createAnxCoreClient } from "$lib/anxCoreClient";
import { buildCoreRequestContextHeaders } from "$lib/coreClientRequestHeaders";
import { buildCoreWorkspaceRoutingHeadersFromSlugs } from "$lib/coreWorkspaceHeadersShared";
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

/** Freeze routing and write identity while a submitted Inbox reply waits for Undo. */
function getInboxResponseClientOptions() {
  const options = getBrowserCoreClientOptions();
  const actorId = options.actorIdProvider();
  const lockActorId = options.lockActorIdProvider();
  const headers = options.requestContextHeadersProvider();
  return {
    actorIdProvider: () => actorId,
    lockActorIdProvider: () => lockActorId,
    requestContextHeadersProvider: () => ({ ...headers }),
  };
}

export function captureInboxResponseSender() {
  const client = createAnxCoreClient({
    ...getInboxResponseClientOptions(),
    fetchFn: (url, init) =>
      globalThis.fetch(url, { ...init, signal: AbortSignal.timeout(30_000) }),
  });
  return client.respondInboxItem.bind(client);
}

/** Reconstruct a persisted mutation's routing and actor without using whichever
 * workspace happens to be on screen when it is retried. Callers gate the scope
 * against the active verified reader before using this client. */
export function createLocalMutationClient(scope) {
  const [organizationSlug, workspaceSlug, agent, actor] = JSON.parse(scope);
  return createAnxCoreClient({
    actorIdProvider: () => actor,
    lockActorIdProvider: () => Boolean(agent),
    requestContextHeadersProvider: () =>
      buildCoreWorkspaceRoutingHeadersFromSlugs({
        organizationSlug,
        workspaceSlug,
      }),
    fetchFn: (url, init) =>
      globalThis.fetch(url, { ...init, signal: AbortSignal.timeout(30_000) }),
  });
}

/** Activation reads keep live auth context while sharing one cancellation scope. */
export function createWorkspaceActivationTransport(signal) {
  const fetchFn = (url, init = {}) =>
    browserReadFetch(url, {
      ...init,
      ...(signal
        ? {
            signal: init.signal
              ? AbortSignal.any([signal, init.signal])
              : signal,
          }
        : {}),
    });
  return {
    fetchFn,
    client: createAnxCoreClient({ ...getBrowserCoreClientOptions(), fetchFn }),
  };
}

/** Bind reads to one reader/workspace and propagate cancellation to fetch. */
export function createWorkspaceReadClient(signal) {
  const fetchFn = browserReadFetch;
  return createAnxCoreClient({
    ...getInboxResponseClientOptions(),
    fetchFn: (url, init) => fetchFn(url, { ...init, signal }),
  });
}

/** Bind all Inbox pages to one scope and abort outstanding reads at its deadline. */
export const createInboxSourceClient = createWorkspaceReadClient;

function resolveBrowserClient() {
  if (!browser) {
    throw new Error(
      "coreClient cannot run during SSR. Use onMount or a load-scoped client created with createAnxCoreClient({ fetchFn: fetch }).",
    );
  }

  if (!browserClient) {
    const fetchFn = browserReadFetch;
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
