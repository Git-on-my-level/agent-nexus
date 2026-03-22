import { redirect } from "@sveltejs/kit";

import {
  getControlClient,
  loadControlSession,
} from "$lib/server/controlSession.js";

export async function load(event) {
  const session = await loadControlSession(event);
  const organizationId = event.url.searchParams.get("organization_id");

  if (!session?.account) {
    const redirectUrl = organizationId
      ? `/auth?redirect=/invites?organization_id=${encodeURIComponent(organizationId)}`
      : "/auth";
    throw redirect(307, redirectUrl);
  }

  let invite = null;
  let inviteError = "";
  let expired = false;
  if (organizationId) {
    try {
      const client = getControlClient(event);
      const response = await client.listOrganizationInvites(organizationId);
      const pendingInvites = (response.invites ?? []).filter(
        (item) => item.status === "pending",
      );
      if (pendingInvites.length === 0) {
        inviteError = "No pending invites found for this organization.";
      } else {
        invite = pendingInvites[0];
        expired =
          invite?.expires_at != null &&
          new Date(invite.expires_at).getTime() < Date.now();
        if (expired) {
          inviteError = "This invite has expired. Please request a new invite.";
        }
      }
    } catch (error) {
      inviteError =
        error instanceof Error ? error.message : "Failed to load invite";
    }
  }

  return {
    organizationId: organizationId || null,
    account: session.account,
    invite,
    inviteError,
    expired,
  };
}
