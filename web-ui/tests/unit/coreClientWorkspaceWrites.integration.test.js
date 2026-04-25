import { describe, expect, it } from "vitest";

import { createAnxCoreClient } from "../../src/lib/anxCoreClient.js";

/**
 * Mirrors browser `coreClient.js`: authenticated workspace user with no selected
 * dev actor. When `/agents/me` returns `agent_id` but an empty `actor_id`
 * (seen after control-plane launch), we must still allow writes: core resolves
 * the actor from the JWT when the body sends `actor_id: ""`.
 */
function workspaceShellClientOptions(fetchFn) {
  return {
    baseUrl: "http://core.test",
    fetchFn,
    actorIdProvider: () => "",
    lockActorIdProvider: () => true,
  };
}

/** Simulates the pre-fix bug: lock used only `getAuthenticatedActorId()` (empty when `/agents/me` omits `actor_id`). */
function brokenOldLockOptions(fetchFn) {
  return {
    baseUrl: "http://core.test",
    fetchFn,
    actorIdProvider: () => "",
    lockActorIdProvider: () => false,
  };
}

describe("coreClient workspace write path (integration)", () => {
  it("createDocument succeeds with empty actor_id when session is locked (agent present)", async () => {
    const bodies = [];
    const client = createAnxCoreClient(
      workspaceShellClientOptions(async (_url, init) => {
        bodies.push(JSON.parse(init.body));
        return new Response(
          JSON.stringify({
            document: { id: "doc-1", title: "Hello" },
            revision: { revision_id: "rev-1" },
          }),
          {
            status: 201,
            headers: { "content-type": "application/json" },
          },
        );
      }),
    );

    await client.createDocument({
      document: { id: "doc-1", title: "Hello" },
      refs: [],
      content: "# Hello",
      content_type: "text",
    });

    expect(bodies).toHaveLength(1);
    expect(bodies[0].actor_id).toBe("");
  });

  it("createDocument throws when lock is off and no actor is selected (regression guard)", async () => {
    const client = createAnxCoreClient(
      brokenOldLockOptions(async () => {
        throw new Error("fetch should not run");
      }),
    );

    await expect(
      client.createDocument({
        document: { id: "doc-1", title: "Hello" },
        refs: [],
        content: "# Hello",
        content_type: "text",
      }),
    ).rejects.toThrow(/No actor selected/);
  });
});
