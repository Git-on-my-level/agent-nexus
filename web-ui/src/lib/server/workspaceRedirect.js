import { redirect } from "@sveltejs/kit";

import {
  workspaceCompositeKey,
  workspacePath,
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths";
import {
  resolveWorkspaceCatalog,
  resolveWorkspaceInRoute,
} from "$lib/server/workspaceResolver";

/** HttpOnly cookie storing `orgSlug:workspaceSlug` for root `/` redirect. */
export const LAST_WORKSPACE_COOKIE = "anx_last_workspace";

function parseLastWorkspaceCookie(raw) {
  const text = String(raw ?? "").trim();
  if (!text) {
    return null;
  }
  const parts = text.split(":");
  if (parts.length >= 2) {
    const org = normalizeOrganizationSlug(parts[0]);
    const ws = normalizeWorkspaceSlug(parts.slice(1).join(":"));
    if (org && ws) {
      return { organizationSlug: org, workspaceSlug: ws };
    }
  }
  return null;
}

/**
 * Root `/` and similar: send the user to their last-used workspace if it still
 * resolves; otherwise the hosted workspace chooser.
 */
export async function redirectToRecentWorkspaceOrChooser(event, pathname = "") {
  await resolveWorkspaceCatalog(event);
  const fromCookie = parseLastWorkspaceCookie(
    event.cookies?.get?.(LAST_WORKSPACE_COOKIE),
  );
  if (fromCookie) {
    const resolved = await resolveWorkspaceInRoute({
      event,
      organizationSlug: fromCookie.organizationSlug,
      workspaceSlug: fromCookie.workspaceSlug,
    });
    if (!resolved.error && resolved.workspace) {
      throw redirect(
        307,
        workspacePath(
          resolved.workspace.organizationSlug,
          resolved.workspace.slug,
          pathname,
        ),
      );
    }
  }

  throw redirect(307, "/hosted/start");
}

export function lastWorkspaceCookieValue(organizationSlug, workspaceSlug) {
  return workspaceCompositeKey(organizationSlug, workspaceSlug);
}
