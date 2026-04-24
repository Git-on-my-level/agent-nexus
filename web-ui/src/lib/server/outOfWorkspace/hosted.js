import { error } from "@sveltejs/kit";

import { normalizeBaseUrl } from "$lib/config.js";
import {
  buildHostedSignInPath,
  normalizeHostedLaunchFinishURL,
  sanitizeHostedReturnPath,
} from "$lib/hosted/launchFlow.js";
import {
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths.js";

import { allowHostedControlPlanePath } from "../hostedControlPlaneAllowlist.js";

import { createControlPlaneClient } from "./cpClient.js";

function mapWorkspaceRowFromControlPlane(match) {
  if (!match || typeof match !== "object") {
    return null;
  }
  const normalizedSlug = normalizeWorkspaceSlug(match.slug);
  if (!normalizedSlug) {
    return null;
  }
  const organizationSlug = normalizeOrganizationSlug(
    match.organization_slug ?? match.organizationSlug ?? "",
  );
  if (!organizationSlug) {
    return null;
  }
  return {
    organizationSlug,
    slug: normalizedSlug,
    label: String(match.display_name ?? match.slug ?? normalizedSlug).trim(),
    description: String(match.description ?? "").trim(),
    coreBaseUrl: normalizeBaseUrl(match.core_origin ?? ""),
    publicOrigin: normalizeBaseUrl(match.public_origin ?? ""),
    id: String(match.id ?? "").trim(),
    workspaceId: String(match.id ?? "").trim(),
    organizationId: String(match.organization_id ?? "").trim(),
    status: String(match.status ?? "").trim(),
    desiredState: String(match.desired_state ?? "").trim(),
  };
}

function canonicalizeSessionExchangeError(httpStatus, cpCode, cpMessage) {
  const msg = String(cpMessage ?? "");
  if (cpCode === "state_mismatch") {
    return "state_mismatch";
  }
  if (cpCode === "exchange_expired") {
    return "exchange_expired";
  }
  if (cpCode === "exchange_invalid") {
    if (httpStatus === 401 || msg.toLowerCase().includes("state is invalid")) {
      return "state_mismatch";
    }
    return "exchange_invalid";
  }
  return cpCode;
}

function defaultSignInPath({ workspaceSlug, workspaceId, returnPath }) {
  return buildHostedSignInPath({
    workspaceSlug,
    workspaceId,
    returnPath,
  });
}

export function createHostedProvider({ controlPlaneBaseUrl, env }) {
  const client = createControlPlaneClient({
    controlPlaneBaseUrl,
    env,
  });

  return {
    mode: "hosted",

    async resolveWorkspaceBySlug({ event, organizationSlug, workspaceSlug }) {
      const lookup = await client.findWorkspaceBySlug({
        event,
        organizationSlug,
        workspaceSlug,
      });
      if (lookup.kind === "unauthenticated") {
        return { kind: "unauthenticated" };
      }
      if (lookup.kind !== "found") {
        return { kind: "missing" };
      }
      const workspace = mapWorkspaceRowFromControlPlane(lookup.workspace);
      if (!workspace) {
        return { kind: "missing" };
      }
      return { kind: "found", workspace };
    },

    async resolveWorkspaceById({ event, workspaceId }) {
      const lookup = await client.getWorkspaceById({ event, workspaceId });
      if (lookup.kind === "unauthenticated") {
        return { kind: "unauthenticated" };
      }
      if (lookup.kind !== "found") {
        return { kind: "missing" };
      }
      const workspace = mapWorkspaceRowFromControlPlane(lookup.workspace);
      if (!workspace) {
        return { kind: "missing" };
      }
      return { kind: "found", workspace };
    },

    async listWorkspacesForOrganization({ event, organizationId }) {
      const result = await client.listWorkspacesByOrganizationId({
        event,
        organizationId,
      });
      if (result.kind !== "ok") {
        return [];
      }
      const out = [];
      for (const row of result.workspaces) {
        const mapped = mapWorkspaceRowFromControlPlane(row);
        if (mapped && mapped.coreBaseUrl) {
          out.push(mapped);
        }
      }
      return out;
    },

    async beginLaunchSession({
      event,
      workspaceId,
      returnPath,
      workspaceSlug,
    }) {
      const launch = await client.createLaunchSession({
        event,
        workspaceId,
        returnPath: sanitizeHostedReturnPath(returnPath, "/"),
      });
      if (launch.status === 401 || launch.status === 403) {
        return {
          kind: "needs_signin",
          signInUrl: defaultSignInPath({
            workspaceSlug,
            workspaceId,
            returnPath,
          }),
        };
      }
      if (!launch.ok) {
        const message = String(
          launch.body?.error?.message ??
            "Could not create control-plane launch session.",
        );
        throw error(launch.status || 502, message);
      }
      const finishRaw = String(
        launch.body?.launch_session?.finish_url ?? "",
      ).trim();
      const finishUrl = normalizeHostedLaunchFinishURL(finishRaw);
      if (!finishUrl) {
        throw error(
          502,
          "Control plane response did not include launch finish URL.",
        );
      }
      return { kind: "redirect", finishUrl };
    },

    async exchangeLaunchSession({ event, request }) {
      const exchanged = await client.exchangeLaunchSession({
        event,
        workspaceId: request.workspaceId,
        exchangeToken: request.exchangeToken,
        state: request.state,
      });
      if (!exchanged.ok) {
        const cpCode = String(
          exchanged.body?.error?.code ?? "session_exchange_failed",
        );
        const cpMessage = String(
          exchanged.body?.error?.message ??
            "Failed to exchange launch session with control plane.",
        );
        return {
          ok: false,
          status: exchanged.status || 502,
          code: canonicalizeSessionExchangeError(
            exchanged.status || 502,
            cpCode,
            cpMessage,
          ),
          message: cpMessage,
        };
      }

      const assertion = String(
        exchanged.body?.grant?.bearer_token ?? "",
      ).trim();
      if (!assertion) {
        return {
          ok: false,
          status: 502,
          code: "invalid_control_plane_response",
          message:
            "Control plane response did not include a workspace grant token.",
        };
      }
      return {
        ok: true,
        assertion,
      };
    },

    buildSignInUrl({ workspaceSlug, workspaceId, returnPath } = {}) {
      return defaultSignInPath({
        workspaceSlug,
        workspaceId,
        returnPath,
      });
    },

    async proxyHostedApi({ event, method, subpath }) {
      const base = normalizeBaseUrl(controlPlaneBaseUrl);
      if (!base) {
        throw error(503, "Control plane URL is not configured");
      }

      const cleanedSubpath = String(subpath ?? "")
        .replace(/^\/+/, "")
        .replace(/\/+$/, "");
      if (!allowHostedControlPlanePath(cleanedSubpath)) {
        throw error(403, "Forbidden");
      }

      const target = `${base}/${cleanedSubpath}${event.url.search}`;
      const headers = new Headers();
      const origin = event.request.headers.get("origin");
      if (origin) {
        headers.set("origin", origin);
      }
      const forwardedAuth = event.request.headers.get("authorization");
      const cookieToken = String(
        event.cookies.get("oar_cp_dev_access_token") ?? "",
      ).trim();
      if (forwardedAuth) {
        headers.set("authorization", forwardedAuth);
      } else if (cookieToken) {
        headers.set("authorization", `Bearer ${cookieToken}`);
      }
      const contentType = event.request.headers.get("content-type");
      if (contentType) {
        headers.set("content-type", contentType);
      }

      /** @type {RequestInit} */
      const init = { method, headers };
      if (method !== "GET" && method !== "HEAD") {
        const body = await event.request.arrayBuffer();
        if (body.byteLength > 0) {
          init.body = body;
        }
      }

      const response = await fetch(target, init);
      const outHeaders = new Headers(response.headers);
      outHeaders.delete("content-encoding");
      outHeaders.delete("transfer-encoding");
      return new Response(response.body, {
        status: response.status,
        statusText: response.statusText,
        headers: outHeaders,
      });
    },

    describeShellCapabilities() {
      const publicAccountPath =
        String(env?.PUBLIC_ANX_HOSTED_ACCOUNT_PATH ?? "").trim() ||
        "/hosted/onboarding";
      const publicOrigin = normalizeBaseUrl(
        String(env?.PUBLIC_ANX_CP_ORIGIN ?? controlPlaneBaseUrl).trim(),
      );
      return {
        mode: "hosted",
        accountPath: publicAccountPath,
        publicOrigin: publicOrigin || null,
        allowsEmptyStaticCatalog: true,
      };
    },
  };
}
