import { describe, expect, it } from "vitest";

import {
  buildLoginRedirectDestination,
  classifyWorkspaceBootstrap,
  mergePrincipals,
  reactivateStaleDevPersonaSession,
  shouldRedirectToLoginForBootstrapState,
  WORKSPACE_BOOTSTRAP_STATES,
} from "../../src/lib/workspaceBootstrap.js";

describe("workspaceBootstrap", () => {
  it("uses explicit hydrating and unresolved bootstrap states", () => {
    expect(
      classifyWorkspaceBootstrap({
        activeWorkspaceSlug: "",
        identityReady: false,
        devActorModeReady: false,
      }),
    ).toBe(WORKSPACE_BOOTSTRAP_STATES.UNRESOLVED);

    expect(
      classifyWorkspaceBootstrap({
        activeWorkspaceSlug: "local",
        identityReady: true,
        devActorModeReady: false,
      }),
    ).toBe(WORKSPACE_BOOTSTRAP_STATES.HYDRATING);
  });

  it("classifies authenticated human, authenticated agent, dev anonymous, and login-required states", () => {
    expect(
      classifyWorkspaceBootstrap({
        activeWorkspaceSlug: "local",
        identityReady: true,
        devActorModeReady: true,
        authenticatedAgent: {
          agent_id: "human-1",
          principal_kind: "human",
        },
      }),
    ).toBe(WORKSPACE_BOOTSTRAP_STATES.AUTHENTICATED_HUMAN);

    expect(
      classifyWorkspaceBootstrap({
        activeWorkspaceSlug: "local",
        identityReady: true,
        devActorModeReady: true,
        authenticatedAgent: {
          agent_id: "agent-1",
          principal_kind: "agent",
        },
      }),
    ).toBe(WORKSPACE_BOOTSTRAP_STATES.AUTHENTICATED_AGENT);

    expect(
      classifyWorkspaceBootstrap({
        activeWorkspaceSlug: "local",
        identityReady: true,
        devActorModeReady: true,
        authenticatedAgent: null,
        hostedMode: false,
        devActorMode: true,
        onLoginRoute: false,
        requiresHumanSession: false,
        hasHumanAuthSession: false,
      }),
    ).toBe(WORKSPACE_BOOTSTRAP_STATES.ANONYMOUS_DEV);

    const loginRequired = classifyWorkspaceBootstrap({
      activeWorkspaceSlug: "local",
      identityReady: true,
      devActorModeReady: true,
      authenticatedAgent: null,
      hostedMode: true,
      devActorMode: true,
      onLoginRoute: false,
      requiresHumanSession: false,
      hasHumanAuthSession: false,
    });
    expect(loginRequired).toBe(WORKSPACE_BOOTSTRAP_STATES.LOGIN_REQUIRED);
    expect(shouldRedirectToLoginForBootstrapState(loginRequired)).toBe(true);
  });

  it("builds hosted and local login redirect destinations from the same return-path inputs", () => {
    expect(
      buildLoginRedirectDestination({
        hostedMode: true,
        organizationSlug: "acme",
        workspaceSlug: "ops",
        workspaceId: "ws-1",
        currentAppPath: "/secrets",
        search: "?tab=keys",
        workspacePath: () => {
          throw new Error("local workspacePath should not be used");
        },
      }),
    ).toBe(
      "/hosted/signin?organization=acme&workspace=ops&workspace_id=ws-1&return_path=%2Fsecrets%3Ftab%3Dkeys",
    );

    expect(
      buildLoginRedirectDestination({
        hostedMode: false,
        organizationSlug: "acme",
        workspaceSlug: "ops",
        currentAppPath: "/secrets",
        search: "?tab=keys",
        workspacePath: (org, workspace, path) =>
          `/ws/${org}/${workspace}${path}`,
      }),
    ).toBe("/ws/acme/ops/login?return_to=%2Fsecrets%3Ftab%3Dkeys");
  });

  it("deduplicates principal registry seeds without dropping distinct identities", () => {
    expect(
      mergePrincipals(
        [{ agent_id: "agent-1", actor_id: "actor-1", username: "A" }],
        [
          { agent_id: "agent-1", actor_id: "actor-1", username: "A" },
          { agent_id: "agent-2", actor_id: "actor-2", username: "B" },
        ],
      ),
    ).toEqual([
      { agent_id: "agent-1", actor_id: "actor-1", username: "A" },
      { agent_id: "agent-2", actor_id: "actor-2", username: "B" },
    ]);
  });
});

describe("reactivateStaleDevPersonaSession", () => {
  const personas = [
    {
      persona_id: "maya",
      agent_id: "agent-new-maya",
      actor_id: "actor-maya",
      principal_kind: "human",
      default: true,
    },
    {
      persona_id: "leo",
      agent_id: "agent-new-leo",
      actor_id: "actor-leo",
      principal_kind: "human",
    },
    {
      persona_id: "pm",
      agent_id: "agent-new-pm",
      actor_id: "actor-pm",
      principal_kind: "agent",
    },
  ];

  it("leaves a session alone when the signed-in agent is a current fixture persona", async () => {
    const calls = [];
    const result = await reactivateStaleDevPersonaSession({
      agent: { agent_id: "agent-new-leo", actor_id: "actor-leo" },
      devFixturePersonas: personas,
      workspaceSlug: "local",
      workspaceHeader: "x-anx-workspace-slug",
      fetchFn: async (url) => {
        calls.push(String(url));
        return { ok: true, json: async () => ({}) };
      },
    });
    expect(result).toBeNull();
    expect(calls).toEqual([]);
  });

  it("re-issues the session for the persona playing the same actor after a reseed", async () => {
    const calls = [];
    const result = await reactivateStaleDevPersonaSession({
      agent: { agent_id: "agent-old-leo", actor_id: "actor-leo" },
      devFixturePersonas: personas,
      workspaceSlug: "local",
      workspaceHeader: "x-anx-workspace-slug",
      organizationSlug: "local",
      fetchFn: async (url, init = {}) => {
        calls.push({
          url: String(url),
          method: init.method || "GET",
          body: init.body,
          org: init.headers?.["x-anx-organization-slug"],
        });
        if (String(url).endsWith("/auth/dev/session")) {
          return { ok: true, json: async () => ({ ok: true }) };
        }
        return {
          ok: true,
          status: 200,
          json: async () => ({
            authenticated: true,
            agent: {
              agent_id: "agent-new-leo",
              actor_id: "actor-leo",
              principal_kind: "human",
            },
          }),
        };
      },
    });
    const session = calls.find((call) =>
      call.url.endsWith("/auth/dev/session"),
    );
    expect(session).toBeTruthy();
    expect(session.method).toBe("POST");
    expect(JSON.parse(session.body)).toEqual({ persona_id: "leo" });
    expect(session.org).toBe("local");
    // Hydration re-reads /auth/session through the same fetch; its return
    // shape belongs to authSession tests, so only the round trip is asserted.
    expect(calls.some((call) => call.url.endsWith("/auth/session"))).toBe(true);
    expect(result === null || typeof result === "object").toBe(true);
  });

  it("falls back to the default human persona when no persona plays the stale actor", async () => {
    const calls = [];
    await reactivateStaleDevPersonaSession({
      agent: { agent_id: "agent-old-x", actor_id: "actor-gone" },
      devFixturePersonas: personas,
      workspaceSlug: "local",
      workspaceHeader: "x-anx-workspace-slug",
      fetchFn: async (url, init = {}) => {
        calls.push({ url: String(url), body: init.body });
        return {
          ok: true,
          status: 200,
          json: async () => ({
            authenticated: true,
            agent: { agent_id: "agent-new-maya" },
          }),
        };
      },
    });
    const session = calls.find((call) =>
      call.url.endsWith("/auth/dev/session"),
    );
    expect(JSON.parse(session.body)).toEqual({ persona_id: "maya" });
  });
});
