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
    const workspaces = await client.listWorkspaces();

    return {
      organizations: organizations.organizations ?? [],
      workspaces: (workspaces.workspaces ?? []).map((workspace) => ({
        ...workspace,
        organization:
          organizations.organizations.find(
            (org) => org.id === workspace.organization_id,
          ) || null,
      })),
      account: session.account,
    };
  } catch {
    return {
      organizations: [],
      workspaces: [],
      account: session.account,
    };
  }
}
