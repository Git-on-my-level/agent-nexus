import { env as privateEnv } from "$env/dynamic/private";

import { normalizeBaseUrl } from "$lib/config.js";

import { createExtensionProvider } from "$lib/server/extensions/provider.js";

/** @type {import("./contract.js").OutOfWorkspaceProvider | null} */
let cachedProvider = null;
/** @type {unknown} */
let cachedEnvRef = null;
let cachedControlBaseUrl = "";

export function createOutOfWorkspaceProvider(env = privateEnv) {
  return createExtensionProvider(env);
}

export function getOutOfWorkspaceProvider(env = privateEnv) {
  const controlPlaneBaseUrl = normalizeBaseUrl(env?.ANX_CONTROL_BASE_URL ?? "");
  if (
    cachedProvider &&
    cachedEnvRef === env &&
    cachedControlBaseUrl === controlPlaneBaseUrl
  ) {
    return cachedProvider;
  }

  cachedProvider = createOutOfWorkspaceProvider(env);
  cachedEnvRef = env;
  cachedControlBaseUrl = controlPlaneBaseUrl;
  return cachedProvider;
}

export function __resetOutOfWorkspaceProviderCacheForTests() {
  cachedProvider = null;
  cachedEnvRef = null;
  cachedControlBaseUrl = "";
}
