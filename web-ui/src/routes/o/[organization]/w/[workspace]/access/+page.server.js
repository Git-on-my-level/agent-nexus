import { normalizeBaseUrl } from "$lib/config";
import { resolveWorkspaceInRoute } from "$lib/server/workspaceResolver";
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

function resolveWorkspacePublicUrl(resolved, requestPath = "") {
  const publicOrigin = normalizeBaseUrl(resolved.workspace?.publicOrigin ?? "");
  if (!publicOrigin) {
    return "";
  }

  const targetPath = String(requestPath ?? "").endsWith("/access")
    ? requestPath.slice(0, -7)
    : workspacePath(resolved.organizationSlug, resolved.workspaceSlug);

  try {
    return normalizeBaseUrl(new URL(targetPath, `${publicOrigin}/`));
  } catch {
    return publicOrigin;
  }
}

/**
 * Base URL for `anx --base-url` in copied CLI commands (anx-core API origin).
 * Prefer the workspace `coreBaseUrl` from the catalog. When it is missing,
 * fall back to public/browser workspace URLs (degraded: operators should set
 * `coreBaseUrl` on `ANX_WORKSPACES` entries so copied commands hit the API).
 */
function resolveRegistrationBaseUrl(event, resolved) {
  const core = normalizeBaseUrl(resolved.workspace?.coreBaseUrl ?? "");
  if (core) {
    return core;
  }

  const requestPath = String(event?.url?.pathname ?? "");
  const browserWorkspaceUrl = requestPath.endsWith("/access")
    ? normalizeBaseUrl(`${event.url.origin}${requestPath.slice(0, -7)}`)
    : "";

  if (browserWorkspaceUrl && !isLoopbackHost(event?.url?.hostname)) {
    return browserWorkspaceUrl;
  }

  const publicWorkspaceUrl = resolveWorkspacePublicUrl(resolved, requestPath);
  if (publicWorkspaceUrl) {
    return publicWorkspaceUrl;
  }

  if (requestPath.endsWith("/access")) {
    return browserWorkspaceUrl;
  }

  try {
    const workspaceUrl = new URL(
      workspacePath(resolved.organizationSlug, resolved.workspaceSlug),
      event.url,
    ).toString();
    return normalizeBaseUrl(workspaceUrl);
  } catch {
    return normalizeBaseUrl(
      resolved.workspace?.publicOrigin ?? resolved.workspace?.coreBaseUrl ?? "",
    );
  }
}

export async function load(event) {
  const resolved = await resolveWorkspaceInRoute({
    event,
    organizationSlug: event.params.organization,
    workspaceSlug: event.params.workspace,
  });
  return {
    coreBaseUrl: resolved.workspace?.coreBaseUrl ?? "",
    workspaceId:
      resolved.workspace?.workspaceId ?? resolved.workspace?.id ?? "",
    registrationBaseUrl: resolveRegistrationBaseUrl(event, resolved),
  };
}
