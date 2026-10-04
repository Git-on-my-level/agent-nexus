import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const loadAdapter = createRequire(import.meta.url);

/** Optional deployment-owned session establishment. No adapter means native auth. */
export async function initializeSessionAdapter(event, env, dev) {
  const moduleURL = env.ANX_UI_SESSION_ADAPTER;
  if (!moduleURL) return;
  // Native Node loading keeps deployment modules outside the OSS bundle.
  const adapter = loadAdapter(fileURLToPath(moduleURL));
  const context = await adapter.createSessionContext({ event, env, dev });
  if (!context || !/^[a-f0-9]{64}$/.test(context.scope))
    throw new Error("Session adapter must provide an opaque request scope");
  scopeWorkspaceCookies(event, context.scope, context);
  event.locals.sessionAdapter = context;
}

/**
 * Every token write is bound to the immutable scope captured at request start.
 * Late responses cannot replace the current scope's cookies. Unscoped legacy
 * cookies are intentionally invisible; no cross-account migration is allowed.
 */
export function scopeWorkspaceCookies(event, scope, codec = {}) {
  const cookies = event.cookies;
  const nameFor = (name) =>
    /^anx_ui_(session|access|auth_retry)_/.test(name) &&
    !/__s_[a-f0-9]{64}$/.test(name)
      ? `${name}__s_${scope}`
      : name;
  event.locals.sessionCookieScope = scope;
  event.cookies = {
    ...cookies,
    get: (name, options) => {
      const scoped = nameFor(name);
      const value = cookies.get(scoped, options);
      return value && scoped !== name && codec.decodeCookie
        ? codec.decodeCookie(scoped, value)
        : value;
    },
    getAll: (options) => cookies.getAll(options),
    set: (name, value, options) => {
      const scoped = nameFor(name);
      return cookies.set(
        scoped,
        scoped !== name && codec.encodeCookie
          ? codec.encodeCookie(scoped, value)
          : value,
        options,
      );
    },
    delete: (name, options) => cookies.delete(nameFor(name), options),
    serialize: (name, value, options) => {
      const scoped = nameFor(name);
      return cookies.serialize(
        scoped,
        scoped !== name && codec.encodeCookie
          ? codec.encodeCookie(scoped, value)
          : value,
        options,
      );
    },
  };
}
