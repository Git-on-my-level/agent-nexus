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
    p === "organizations" ||
    p.startsWith("organizations/") ||
    p.startsWith("billing/") ||
    p === "workspaces" ||
    p.startsWith("workspaces/") ||
    p === "provisioning" ||
    p.startsWith("provisioning/")
  );
}
