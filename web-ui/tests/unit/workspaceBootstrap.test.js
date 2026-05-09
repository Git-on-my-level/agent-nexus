import { describe, expect, it } from "vitest";

import {
  buildLoginRedirectDestination,
  classifyWorkspaceBootstrap,
  mergePrincipals,
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
