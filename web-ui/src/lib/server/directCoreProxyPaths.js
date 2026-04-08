/**
 * Paths implemented by SvelteKit (not proxied to oar-core). Every other API
 * path should be registered in contracts/oar-openapi.yaml so
 * isProxyableCommand matches it.
 *
 * Workspace session, dev fixtures, and passkey ceremony shims must not hit
 * oar-core directly from the browser in ways that collide with core /auth/*
 * routes handled here.
 */

export function isWebUiOwnedAuthPath(pathname) {
  if (pathname === "/auth/session" || pathname.startsWith("/auth/session/")) {
    return true;
  }
  if (pathname === "/auth/dev" || pathname.startsWith("/auth/dev/")) {
    return true;
  }
  if (
    pathname === "/auth/passkey/login/verify" ||
    pathname === "/auth/passkey/register/verify"
  ) {
    return true;
  }
  return false;
}

/**
 * @param {string} method
 * @param {string} pathname
 * @returns {boolean}
 */
export function isDirectCoreProxyPath(method, pathname) {
  void method;
  void pathname;
  return false;
}
