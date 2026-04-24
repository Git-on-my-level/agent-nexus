import { env as privateEnv } from "$env/dynamic/private";

import { normalizeBaseUrl } from "$lib/config.js";

import { createHostedProvider } from "./hosted.js";
import { createLocalProvider } from "./local.js";

/** @type {import("./contract.js").OutOfWorkspaceProvider | null} */
let cachedProvider = null;
/** @type {unknown} */
let cachedEnvRef = null;
let cachedControlBaseUrl = "";

export function createOutOfWorkspaceProvider(env = privateEnv) {
  const controlPlaneBaseUrl = normalizeBaseUrl(env?.ANX_CONTROL_BASE_URL ?? "");
  if (!controlPlaneBaseUrl) {
    return createLocalProvider();
  }
  return createHostedProvider({
    env,
    controlPlaneBaseUrl,
  });
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
