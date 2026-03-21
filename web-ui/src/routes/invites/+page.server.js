import { redirect } from "@sveltejs/kit";

import { loadControlSession } from "$lib/server/controlSession.js";

export async function load(event) {
  const session = await loadControlSession(event);
  const organizationId = event.url.searchParams.get("organization_id");

  if (!session?.account) {
    const redirectUrl = organizationId
      ? `/auth?redirect=/invites?organization_id=${encodeURIComponent(organizationId)}`
      : "/auth";
    throw redirect(307, redirectUrl);
  }

  return {
    organizationId: organizationId || null,
    account: session.account,
  };
}
