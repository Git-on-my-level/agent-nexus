import { normalizeBaseUrl } from "$lib/config";
import { resolveWorkspaceBySlug } from "$lib/server/workspaceResolver";
import { workspacePath } from "$lib/workspacePaths";

function resolveRegistrationBaseUrl(event, resolved) {
  const requestPath = String(event?.url?.pathname ?? "");
  if (requestPath.endsWith("/access")) {
    return normalizeBaseUrl(`${event.url.origin}${requestPath.slice(0, -7)}`);
  }

  try {
    const workspaceUrl = new URL(
      workspacePath(resolved.workspaceSlug),
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
  const resolved = await resolveWorkspaceBySlug({
    event,
    workspaceSlug: event.params.workspace,
  });
  return {
    coreBaseUrl: resolved.workspace?.coreBaseUrl ?? "",
    registrationBaseUrl: resolveRegistrationBaseUrl(event, resolved),
  };
}
