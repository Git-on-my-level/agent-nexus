import { workspacePath } from "$lib/workspacePaths.js";

export function hostedOrgLabel(org) {
  return String(org?.display_name || org?.slug || "the active organization");
}

export async function readHostedCreateError(res, activeOrg) {
  let code = "";
  let detail = "";
  try {
    const j = await res.json();
    code = String(j?.error?.code ?? "").trim();
    detail = String(j?.error?.message || code || res.statusText).trim();
  } catch {
    detail = String(res.statusText ?? "").trim();
  }

  const lower = `${code} ${detail}`.toLowerCase();
  if (
    code === "quota_exceeded" ||
    (lower.includes("quota") && lower.includes("workspace")) ||
    (lower.includes("limit") && lower.includes("workspace"))
  ) {
    return `You've reached ${hostedOrgLabel(activeOrg)}'s workspace limit. Delete an unused workspace or contact us to raise the limit.`;
  }

  return detail || "Failed to create workspace.";
}

export function workspaceCreatedDashboardHref(orgId, workspace) {
  const params = new URLSearchParams();
  if (orgId) params.set("organization_id", String(orgId));
  if (workspace?.id) params.set("created_workspace_id", String(workspace.id));
  if (workspace?.display_name || workspace?.slug) {
    params.set(
      "created_workspace",
      String(workspace.display_name || workspace.slug),
    );
  }
  const query = params.toString();
  return query ? `/hosted/dashboard?${query}` : "/hosted/dashboard";
}

export function workspaceCreateRedirectHref(org, workspace) {
  const status = String(workspace?.status ?? "").toLowerCase();
  const orgSlug = workspace?.organization_slug ?? org?.slug;
  if (status === "ready" && orgSlug && workspace?.slug) {
    return workspacePath(orgSlug, workspace.slug, "/inbox");
  }
  return workspaceCreatedDashboardHref(org?.id, workspace);
}
