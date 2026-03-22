import { get } from "svelte/store";
import { afterEach, describe, expect, it } from "vitest";

import {
  authenticatedAgent,
  clearAuthSession,
  completeAuthSession,
  initializeAuthSession,
  isAuthenticated,
  logoutAuthSession,
} from "../../src/lib/authSession.js";
import { WORKSPACE_HEADER } from "../../src/lib/workspacePaths.js";

afterEach(() => {
  clearAuthSession("local");
  clearAuthSession("alpha");
});

describe("authSession", () => {
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
