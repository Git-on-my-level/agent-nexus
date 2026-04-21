import { initialsFor } from "$lib/hosted/session.js";

/**
 * Build sidebar identity for the workspace shell: control plane shows account
 * profile (display name / email) while anx-core exposes a principal username
 * (often `external.*`). When both exist, prefer CP-friendly labels and show the
 * workspace handle as secondary copy.
 *
 * @param {{
 *   hostedMode: boolean,
 *   hostedAccount: { display_name?: string, email?: string } | null,
 *   selectedActorName: string,
 *   authenticatedAgent: { username?: string, actor_id?: string } | null,
 * }} input
 */
export function computeWorkspaceShellIdentity({
  hostedMode,
  hostedAccount,
  selectedActorName,
  authenticatedAgent,
}) {
  const username = String(authenticatedAgent?.username ?? "").trim();
  const coreDisplay = String(selectedActorName ?? "").trim();

  if (hostedMode && hostedAccount) {
    const displayName = String(hostedAccount.display_name ?? "").trim();
    const email = String(hostedAccount.email ?? "").trim();
    const cpPrimary = displayName || email;
    if (cpPrimary) {
      return {
        primaryLabel: cpPrimary,
        secondaryLabel: username && username !== cpPrimary ? username : "",
        initials: initialsFor(hostedAccount),
      };
    }
  }

  const primary = coreDisplay || username || "Unknown identity";
  return {
    primaryLabel: primary,
    secondaryLabel: "",
    initials: initialsFromLabel(primary),
  };
}

function initialsFromLabel(label) {
  const s = String(label ?? "").trim();
  if (!s) return "?";
  const parts = s.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).slice(0, 2).toUpperCase();
  }
  return s.slice(0, 2).toUpperCase();
}
