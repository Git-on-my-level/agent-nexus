import { get } from "svelte/store";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  authenticatedAgent,
  clearAuthSession,
  completeAuthSession,
  initializeAuthSession,
  isAuthenticated,
  isHumanWorkspacePrincipal,
  logoutAuthSession,
} from "../../src/lib/authSession.js";
import { WORKSPACE_HEADER } from "../../src/lib/workspacePaths.js";

afterEach(() => {
  vi.useRealTimers();
  clearAuthSession("local");
  clearAuthSession("alpha");
});

describe("authSession", () => {
  it("classifies human principals for workspace auth", () => {
    expect(isHumanWorkspacePrincipal(null)).toBe(false);
    expect(
      isHumanWorkspacePrincipal({ agent_id: "a", auth_method: "public_key" }),
    ).toBe(false);
    expect(
      isHumanWorkspacePrincipal({ agent_id: "a", principal_kind: "human" }),
    ).toBe(true);
    expect(
      isHumanWorkspacePrincipal({ agent_id: "a", auth_method: "passkey" }),
    ).toBe(true);
    expect(
      isHumanWorkspacePrincipal({
        agent_id: "a",
        auth_method: "control_plane",
      }),
    ).toBe(true);
    expect(
      isHumanWorkspacePrincipal({
        agent_id: "a",
        auth_method: "external_grant",
      }),
    ).toBe(true);
  });

  it("keeps the authenticated agent in memory", () => {
    completeAuthSession(
      { agent_id: "agent-1", actor_id: "actor-1", username: "passkey.user" },
      "local",
    );

    expect(isAuthenticated("local")).toBe(true);
    expect(get(authenticatedAgent)).toMatchObject({
      agent_id: "agent-1",
      actor_id: "actor-1",
    });
  });

  it("loads the current agent from the same-origin session endpoint", async () => {
    const calls = [];

    const agent = await initializeAuthSession({
      workspaceSlug: "alpha",
      fetchFn: async (url, options = {}) => {
        calls.push({
          url: String(url),
          method: options.method,
          headers: new Headers(options.headers),
        });

        return new Response(
          JSON.stringify({
            authenticated: true,
            agent: {
              agent_id: "agent-2",
              actor_id: "actor-2",
              username: "passkey.agent",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        );
      },
    });

    expect(agent).toMatchObject({
      agent_id: "agent-2",
      actor_id: "actor-2",
    });
    expect(calls).toHaveLength(1);
    expect(calls[0].url.endsWith("/auth/session")).toBe(true);
    expect(calls[0].method).toBe("GET");
    expect(calls[0].headers.get(WORKSPACE_HEADER)).toBe("alpha");
    expect(get(authenticatedAgent)).toMatchObject({
      agent_id: "agent-2",
      actor_id: "actor-2",
    });
  });

  it("preserves the current agent when session rehydration fails transiently", async () => {
    completeAuthSession(
      { agent_id: "agent-9", actor_id: "actor-9", username: "passkey.user" },
      "alpha",
    );

    const agent = await initializeAuthSession({
      workspaceSlug: "alpha",
      fetchFn: async () => {
        throw new Error("core temporarily unavailable");
      },
    });

    expect(agent).toMatchObject({
      agent_id: "agent-9",
      actor_id: "actor-9",
    });
    expect(get(authenticatedAgent)).toMatchObject({
      agent_id: "agent-9",
      actor_id: "actor-9",
    });
  });

  it("retries session rehydration once when auth refresh is still settling", async () => {
    vi.useFakeTimers();
    const calls = [];

    const agentPromise = initializeAuthSession({
      workspaceSlug: "alpha",
      fetchFn: async (url, options = {}) => {
        calls.push({
          url: String(url),
          method: options.method,
          headers: new Headers(options.headers),
        });

        if (calls.length === 1) {
          return new Response(
            JSON.stringify({
              error: {
                code: "auth_session_retryable",
                message: "Workspace authentication refresh is in progress.",
              },
            }),
            {
              status: 503,
              headers: { "content-type": "application/json" },
            },
          );
        }

        return new Response(
          JSON.stringify({
            authenticated: true,
            agent: {
              agent_id: "agent-8",
              actor_id: "actor-8",
              username: "passkey.retry",
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        );
      },
    });

    await vi.runAllTimersAsync();
    const agent = await agentPromise;

    expect(calls).toHaveLength(2);
    expect(agent).toMatchObject({
      agent_id: "agent-8",
      actor_id: "actor-8",
    });
    expect(get(authenticatedAgent)).toMatchObject({
      agent_id: "agent-8",
      actor_id: "actor-8",
    });
  });

  it("clears the current agent when session rehydration fails with a non-retryable response", async () => {
    completeAuthSession(
      { agent_id: "agent-10", actor_id: "actor-10", username: "passkey.user" },
      "alpha",
    );

    const agent = await initializeAuthSession({
      workspaceSlug: "alpha",
      fetchFn: async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "auth_required",
              message: "session expired",
            },
          }),
          {
            status: 401,
            headers: { "content-type": "application/json" },
          },
        ),
    });

    expect(agent).toBeNull();
    expect(isAuthenticated("alpha")).toBe(false);
    expect(get(authenticatedAgent)).toBeNull();
  });

  it("clears the current agent when retryable session rehydration never recovers", async () => {
    vi.useFakeTimers();
    completeAuthSession(
      { agent_id: "agent-11", actor_id: "actor-11", username: "passkey.user" },
      "alpha",
    );

    const agentPromise = initializeAuthSession({
      workspaceSlug: "alpha",
      fetchFn: async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "auth_session_retryable",
              message: "Workspace authentication refresh is in progress.",
            },
          }),
          {
            status: 503,
            headers: { "content-type": "application/json" },
          },
        ),
    });

    await vi.runAllTimersAsync();
    const agent = await agentPromise;

    expect(agent).toBeNull();
    expect(isAuthenticated("alpha")).toBe(false);
    expect(get(authenticatedAgent)).toBeNull();
  });

  it("logs out through the same-origin session endpoint", async () => {
    const calls = [];
    completeAuthSession(
      { agent_id: "agent-3", actor_id: "actor-3", username: "passkey.user" },
      "alpha",
    );

    await logoutAuthSession({
      workspaceSlug: "alpha",
      fetchFn: async (url, options = {}) => {
        calls.push({
          url: String(url),
          method: options.method,
          headers: new Headers(options.headers),
        });
        return new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      },
    });

    expect(calls).toHaveLength(1);
    expect(calls[0].url.endsWith("/auth/session")).toBe(true);
    expect(calls[0].method).toBe("DELETE");
    expect(calls[0].headers.get(WORKSPACE_HEADER)).toBe("alpha");
    expect(isAuthenticated("alpha")).toBe(false);
  });
});
