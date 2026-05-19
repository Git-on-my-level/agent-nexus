/**
 * @param {string} subpath
 * @returns {boolean}
 */
export function allowHostedControlPlanePath(subpath) {
  const p = String(subpath ?? "")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");
  if (!p || p.includes("..")) {
    return false;
  }
  return (
    p.startsWith("account/") ||
    p === "admin/whoami" ||
    p === "admin/analytics/overview" ||
    p === "admin/analytics/organizations" ||
    p.startsWith("admin/analytics/organizations/") ||
    p === "admin/analytics/workspaces" ||
    p.startsWith("admin/analytics/workspaces/") ||
    p === "admin/analytics/accounts" ||
    p.startsWith("admin/analytics/accounts/") ||
    p === "admin/analytics/audit-events" ||
    p === "admin/analytics/operations" ||
    p === "admin/analytics/hosts" ||
    p.startsWith("admin/analytics/hosts/") ||
    p === "organizations" ||
    p.startsWith("organizations/") ||
    p.startsWith("billing/") ||
    p === "mcp/oauth/browser/authorize" ||
    p === "workspaces" ||
    p.startsWith("workspaces/") ||
    p === "provisioning" ||
    p.startsWith("provisioning/")
  );
}
