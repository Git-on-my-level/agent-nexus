import { normalizeBaseUrl } from "$lib/config";
import { workspacePath } from "$lib/workspacePaths";

function isLoopbackHost(hostname) {
  const normalized = String(hostname ?? "")
    .trim()
    .toLowerCase();
  return (
    normalized === "localhost" ||
    normalized.endsWith(".localhost") ||
    normalized === "127.0.0.1" ||
    normalized === "0.0.0.0" ||
    normalized === "::1" ||
    normalized === "[::1]"
  );
}

/**
 * The workspace's own root path, which is where a CLI base URL points.
 *
 * Read from the request path when it is in there, so a deployment served
 * under a base path keeps it: `ANX_UI_BASE_PATH` is read at module load and a
 * reverse proxy can add a prefix this process never sees. Any route under the
 * workspace works, not just one known suffix.
 */
function workspaceRootPath(event, resolved) {
  const org = String(
    resolved?.organizationSlug ?? resolved?.workspace?.organizationSlug ?? "",
  ).trim();
  const slug = String(
    resolved?.workspaceSlug ?? resolved?.workspace?.slug ?? "",
  ).trim();
  if (!org || !slug) {
    return "";
  }

  /*
   * Match the workspace segments as they appear in the request rather than
   * comparing them to the catalog's slugs: the URL is normalized (case
   * folded, punctuation collapsed) before resolution, so `/w/Ops_v2` resolves
   * to `ops-v2` and any equality test would miss it — and with it any path
   * prefix a reverse proxy added that this process never sees.
   */
  const pathname = String(event?.url?.pathname ?? "");
  const matched = /\/o\/[^/]+\/w\/[^/]+/.exec(pathname);
  if (matched) {
    return pathname.slice(0, matched.index + matched[0].length);
  }

  try {
    return workspacePath(org, slug);
  } catch {
    return "";
  }
}

/**
 * Base URL for `anx --base-url` in copied CLI commands (anx-core API origin).
 *
 * Prefer the workspace `coreBaseUrl` from the catalog. When it is missing,
 * fall back to the public or browser workspace URL (degraded: operators
 * should set `coreBaseUrl` on `ANX_WORKSPACES` entries so copied commands hit
 * the API). A loopback origin is used only as a last resort, because a command
 * the reader copies is usually run on another machine.
 *
 * @param {{ url?: URL }} event SvelteKit request event
 * @param {{ organizationSlug?: string, workspaceSlug?: string, workspace?: object }} resolved
 */
export function resolveCliBaseUrl(event, resolved) {
  const core = normalizeBaseUrl(resolved?.workspace?.coreBaseUrl ?? "");
  if (core) {
    return core;
  }

  const rootPath = workspaceRootPath(event, resolved);

  if (rootPath && event?.url?.origin && !isLoopbackHost(event.url.hostname)) {
    return normalizeBaseUrl(`${event.url.origin}${rootPath}`);
  }

  const publicOrigin = normalizeBaseUrl(
    resolved?.workspace?.publicOrigin ?? "",
  );
  if (publicOrigin) {
    try {
      return normalizeBaseUrl(new URL(rootPath, `${publicOrigin}/`));
    } catch {
      return publicOrigin;
    }
  }

  if (rootPath && event?.url) {
    try {
      return normalizeBaseUrl(new URL(rootPath, event.url).toString());
    } catch {
      // Fall through to whatever the catalog knows.
    }
  }

  return normalizeBaseUrl(
    resolved?.workspace?.publicOrigin ?? resolved?.workspace?.coreBaseUrl ?? "",
  );
}
