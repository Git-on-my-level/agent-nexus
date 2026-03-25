import { env } from "$env/dynamic/private";
import { OarClient } from "../../../../contracts/gen/control/ts/dist/client.js";

export function getControlBaseUrl() {
  return (
    env.OAR_CONTROL_BASE_URL ||
    env.PUBLIC_OAR_CONTROL_BASE_URL ||
    "http://127.0.0.1:8100"
  );
}

function parseResponseBody(result) {
  if (!result.body) {
    return {};
  }

  try {
    return JSON.parse(result.body);
  } catch {
    return { raw: result.body };
  }
}

function createControlError(result, message) {
  const error = new Error(message);
  error.status = result.status;
  error.body = parseResponseBody(result);
  return error;
}

export function createControlClient(accessToken, extraHeaders = {}) {
  const baseUrl = getControlBaseUrl();
  const client = new OarClient(baseUrl, fetch);

  const headers = {
    ...(accessToken ? { authorization: `Bearer ${accessToken}` } : {}),
    ...Object.fromEntries(
      Object.entries(extraHeaders).filter(([, value]) => {
        return String(value ?? "").trim() !== "";
      }),
    ),
  };

  return {
    async startPasskeyRegistration(body) {
      const result = await client.controlAccountsPasskeysRegisterStart({
        body,
        headers,
      });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to start registration");
      }
      return parseResponseBody(result);
    },

    async finishPasskeyRegistration(body) {
      const result = await client.controlAccountsPasskeysRegisterFinish({
        body,
        headers,
      });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to finish registration");
      }
      return parseResponseBody(result);
    },

    async startSession(body) {
      const result = await client.controlAccountsSessionsStart({
        body,
        headers,
      });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to start session");
      }
      return parseResponseBody(result);
    },

    async finishSession(body) {
      const result = await client.controlAccountsSessionsFinish({
        body,
        headers,
      });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to finish session");
      }
      return parseResponseBody(result);
    },

    async revokeCurrentSession() {
      const result = await client.controlAccountsSessionsRevokeCurrent({
        headers,
      });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to revoke session");
      }
      return parseResponseBody(result);
    },

    async validateSession() {
      const result = await client.controlOrganizationsList({ headers });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to validate session");
      }
      return true;
    },

    async listOrganizations() {
      const result = await client.controlOrganizationsList({ headers });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to list organizations");
      }
      return parseResponseBody(result);
    },

    async createOrganization(body) {
      const result = await client.controlOrganizationsCreate({ body, headers });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to create organization");
      }
      return parseResponseBody(result);
    },

    async getOrganization(organizationId) {
      const result = await client.controlOrganizationsGet(
        { organization_id: organizationId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to get organization");
      }
      return parseResponseBody(result);
    },

    async updateOrganization(organizationId, body) {
      const result = await client.controlOrganizationsUpdate(
        { organization_id: organizationId },
        { body, headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to update organization");
      }
      return parseResponseBody(result);
    },

    async listOrganizationMemberships(organizationId) {
      const result = await client.controlOrganizationsMembershipsList(
        { organization_id: organizationId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to list memberships");
      }
      return parseResponseBody(result);
    },

    async updateOrganizationMembership(organizationId, membershipId, body) {
      const result = await client.controlOrganizationsMembershipsUpdate(
        { organization_id: organizationId, membership_id: membershipId },
        { body, headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to update membership");
      }
      return parseResponseBody(result);
    },

    async listOrganizationInvites(organizationId) {
      const result = await client.controlOrganizationsInvitesList(
        { organization_id: organizationId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to list invites");
      }
      return parseResponseBody(result);
    },

    async createOrganizationInvite(organizationId, body) {
      const result = await client.controlOrganizationsInvitesCreate(
        { organization_id: organizationId },
        { body, headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to create invite");
      }
      return parseResponseBody(result);
    },

    async revokeOrganizationInvite(organizationId, inviteId) {
      const result = await client.controlOrganizationsInvitesRevoke(
        { organization_id: organizationId, invite_id: inviteId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to revoke invite");
      }
      return parseResponseBody(result);
    },

    async getOrganizationUsageSummary(organizationId) {
      const result = await client.controlOrganizationsUsageSummaryGet(
        { organization_id: organizationId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to get usage summary");
      }
      return parseResponseBody(result);
    },

    async listWorkspaces(organizationId) {
      const result = await client.controlWorkspacesList({
        query: organizationId ? { organization_id: organizationId } : undefined,
        headers,
      });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to list workspaces");
      }
      return parseResponseBody(result);
    },

    async createWorkspace(body) {
      const result = await client.controlWorkspacesCreate({ body, headers });
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to create workspace");
      }
      return parseResponseBody(result);
    },

    async getWorkspace(workspaceId) {
      const result = await client.controlWorkspacesGet(
        { workspace_id: workspaceId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to get workspace");
      }
      return parseResponseBody(result);
    },

    async getProvisioningJob(jobId) {
      const result = await client.controlProvisioningJobsGet(
        { job_id: jobId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to get provisioning job");
      }
      return parseResponseBody(result);
    },

    async createLaunchSession(workspaceId, body = {}) {
      const result = await client.controlWorkspacesLaunchSessionsCreate(
        { workspace_id: workspaceId },
        { body, headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to create launch session");
      }
      return parseResponseBody(result);
    },

    async exchangeWorkspaceSession(workspaceId, exchangeToken) {
      const result = await client.controlWorkspacesSessionExchangeCreate(
        { workspace_id: workspaceId },
        { body: { exchange_token: exchangeToken } },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to exchange session");
      }
      return parseResponseBody(result);
    },

    async suspendWorkspace(workspaceId) {
      const result = await client.controlWorkspacesSuspend(
        { workspace_id: workspaceId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to suspend workspace");
      }
      return parseResponseBody(result);
    },

    async resumeWorkspace(workspaceId) {
      const result = await client.controlWorkspacesResume(
        { workspace_id: workspaceId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to resume workspace");
      }
      return parseResponseBody(result);
    },

    async decommissionWorkspace(workspaceId) {
      const result = await client.controlWorkspacesDecommission(
        { workspace_id: workspaceId },
        { headers },
      );
      if (!result.status || result.status >= 400) {
        throw createControlError(result, "Failed to decommission workspace");
      }
      return parseResponseBody(result);
    },
  };
}
