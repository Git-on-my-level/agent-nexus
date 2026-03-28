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
    const billingSummaries = await Promise.all(
      (organizations.organizations ?? []).map(async (organization) => {
        try {
          const response = await client.getOrganizationBillingSummary(
            organization.id,
          );
          return [organization.id, response.summary ?? null];
        } catch {
          return [organization.id, null];
        }
      }),
    );

    return {
      organizations: organizations.organizations ?? [],
      billingByOrganization: Object.fromEntries(billingSummaries),
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
      billingByOrganization: {},
      workspaces: [],
      account: session.account,
    };
  }
}
