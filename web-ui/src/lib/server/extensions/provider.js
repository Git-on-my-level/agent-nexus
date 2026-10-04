import { createLocalProvider } from "$lib/server/outOfWorkspace/local.js";

// A composed application may replace this factory with an external provider.
export function createExtensionProvider() {
  return createLocalProvider();
}
