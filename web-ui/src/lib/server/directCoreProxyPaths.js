/**
 * Same-origin paths proxied to core even when missing from the generated
 * command catalog. Keep this list small; prefer registering operations in
 * contracts/oar-openapi.yaml when they are stable workspace APIs.
 *
 * Several `/auth/*` routes are implemented in SvelteKit (cookie-backed workspace
 * session, dev fixture identities, passkey ceremony shims). Those must not be
 * proxied — oar-core does not serve them and will return 4xx.
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

export function isDirectCoreAuthPath(pathname) {
  if (isWebUiOwnedAuthPath(pathname)) {
    return false;
  }
  return pathname.startsWith("/auth/");
}

/** GET/POST /actors — utility listing; not in generated catalog today. */
export function isDirectCoreActorsPath(method, pathname) {
  const upper = method.toUpperCase();
  return pathname === "/actors" && (upper === "GET" || upper === "POST");
}

/**
 * @param {string} method
 * @param {string} pathname
 * @returns {boolean}
 */
export function isDirectCoreProxyPath(method, pathname) {
  const upper = method.toUpperCase();
  return (
    (upper === "GET" && pathname === "/meta/handshake") ||
    isDirectCoreAuthPath(pathname) ||
    isDirectCoreActorsPath(method, pathname)
  );
}
