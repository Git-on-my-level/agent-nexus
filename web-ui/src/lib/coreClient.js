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
export function getInboxResponseClientOptions() {
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
    fetchFn: globalThis.fetch.bind(globalThis),
  });
  return client.respondInboxItem.bind(client);
}

/** Bind all Inbox pages to one scope and abort outstanding reads at its deadline. */
export function createInboxSourceClient(signal) {
  const fetchFn = globalThis.fetch.bind(globalThis);
  return createAnxCoreClient({
    ...getInboxResponseClientOptions(),
    fetchFn: (url, init) => fetchFn(url, { ...init, signal }),
  });
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

/**
 * A client bound to a workspace other than the one on screen.
 *
 * The browser reaches core through a same-origin proxy that routes on two
 * headers, so reading another workspace is the same session with different
 * routing — which is what lets the Overview's urgent band ask every workspace
 * the reader can reach whether anything is waiting for them, without a second
 * login per workspace.
 *
 * Clients are cached per workspace: the band re-reads on refresh, and building
 * a new client each time would throw away the command-registry check it does
 * on construction.
 *
 * @param {{ organizationSlug?: string, workspaceSlug?: string }} target
 */
const workspaceScopedClients = new Map();

export function workspaceScopedCoreClient({
  organizationSlug = "",
  workspaceSlug = "",
} = {}) {
  if (!browser) {
    throw new Error(
      "workspaceScopedCoreClient cannot run during SSR. Use a load-scoped client created with createAnxCoreClient({ fetchFn: fetch }).",
    );
  }
  const org = String(organizationSlug ?? "").trim();
  const workspace = String(workspaceSlug ?? "").trim();
  const key = `${org}/${workspace}`;
  let client = workspaceScopedClients.get(key);
  if (!client) {
    client = createAnxCoreClient({
      actorIdProvider: () => getAuthenticatedActorId() || getSelectedActorId(),
      lockActorIdProvider: () => Boolean(getAuthenticatedAgent()?.agent_id),
      requestContextHeadersProvider: () =>
        buildCoreWorkspaceRoutingHeadersFromSlugs({
          organizationSlug: org,
          workspaceSlug: workspace,
        }),
      fetchFn: globalThis.fetch.bind(globalThis),
    });
    workspaceScopedClients.set(key, client);
  }
  return client;
}
