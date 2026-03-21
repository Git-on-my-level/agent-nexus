import { redirect } from "@sveltejs/kit";

import {
  loadControlSession,
  getControlClient,
} from "$lib/server/controlSession.js";

export async function load(event) {
  const session = await loadControlSession(event);

  if (!session?.account) {
    throw redirect(307, "/auth");
  }

  try {
    const client = getControlClient(event);
    const organizations = await client.listOrganizations();

    return {
      organizations: organizations.organizations ?? [],
      account: session.account,
    };
  } catch {
    return {
      organizations: [],
      account: session.account,
    };
  }
}
