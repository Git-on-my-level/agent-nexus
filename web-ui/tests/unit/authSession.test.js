import { get } from "svelte/store";
import { afterEach, describe, expect, it } from "vitest";

import {
  REFRESH_TOKEN_STORAGE_KEY,
  authenticatedAgent,
  clearAuthSession,
  completeAuthSession,
  loadStoredRefreshToken,
  refreshAuthSession,
} from "../../src/lib/authSession.js";

function createMemoryStorage() {
  const data = new Map();

  return {
    getItem(key) {
      return data.has(key) ? data.get(key) : null;
    },
    setItem(key, value) {
      data.set(key, String(value));
    },
    removeItem(key) {
      data.delete(key);
    },
  };
}

afterEach(() => {
  clearAuthSession(createMemoryStorage());
});

describe("authSession", () => {
  it("stores the refresh token in session storage and keeps the agent in memory", () => {
    const storage = createMemoryStorage();

    completeAuthSession(
      { agent_id: "agent-1", actor_id: "actor-1", username: "passkey.user" },
      {
        access_token: "access-1",
        refresh_token: "refresh-1",
      },
      storage,
    );

    expect(loadStoredRefreshToken(storage)).toBe("refresh-1");
    expect(storage.getItem(REFRESH_TOKEN_STORAGE_KEY)).toBe("refresh-1");
    expect(get(authenticatedAgent)).toMatchObject({
      agent_id: "agent-1",
      actor_id: "actor-1",
    });
  });

  it("refreshes tokens and reloads the current agent profile", async () => {
    const storage = createMemoryStorage();
    storage.setItem(REFRESH_TOKEN_STORAGE_KEY, "refresh-old");

    const calls = [];
    const result = await refreshAuthSession({
      storage,
      fetchFn: async (url, options = {}) => {
        calls.push({
          url: String(url),
          method: options.method,
          headers: new Headers(options.headers),
          body: options.body ? JSON.parse(options.body) : null,
        });

        if (String(url).endsWith("/auth/token")) {
          return new Response(
            JSON.stringify({
              tokens: {
                access_token: "access-new",
                refresh_token: "refresh-new",
              },
            }),
            {
              status: 200,
              headers: { "content-type": "application/json" },
            },
          );
        }

        if (String(url).endsWith("/agents/me")) {
          return new Response(
            JSON.stringify({
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
        }

        return new Response("not found", { status: 404 });
      },
    });

    expect(result).toMatchObject({
      agent: { agent_id: "agent-2", actor_id: "actor-2" },
      tokens: { access_token: "access-new", refresh_token: "refresh-new" },
    });
    expect(loadStoredRefreshToken(storage)).toBe("refresh-new");
    expect(
      calls
        .find((call) => call.url.endsWith("/agents/me"))
        .headers.get("authorization"),
    ).toBe("Bearer access-new");
  });
});
