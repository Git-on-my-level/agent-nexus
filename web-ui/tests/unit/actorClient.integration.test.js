import { describe, expect, it } from "vitest";

import { chooseActor } from "../../src/lib/actorSession.js";
import { createOarCoreClient } from "../../src/lib/anxCoreClient.js";

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

describe("actor flow integration (mocked)", () => {
  it("registers actor, selects actor, and posts event with actor_id", async () => {
    const storage = createMemoryStorage();
    let selectedActorId = "";
    const calls = [];

    const fetchFn = async (url, options) => {
      calls.push({
        url: String(url),
        method: options.method,
        body: options.body ? JSON.parse(options.body) : undefined,
      });

      if (String(url).endsWith("/actors") && options.method === "POST") {
        return new Response(
          JSON.stringify({ actor: calls.at(-1).body.actor }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        );
      }

      if (String(url).endsWith("/events") && options.method === "POST") {
        return new Response(
          JSON.stringify({
            event: {
              id: "event-1",
              actor_id: calls.at(-1).body.actor_id,
              ...calls.at(-1).body.event,
            },
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" },
          },
        );
      }

      return new Response("not found", {
        status: 404,
        statusText: "Not Found",
      });
    };

    const client = createOarCoreClient({
      baseUrl: "http://anx-core.test",
      fetchFn,
      actorIdProvider: () => selectedActorId,
    });

    const actorResponse = await client.createActor({
      actor: {
        id: "actor-alex",
        display_name: "Alex",
        tags: ["human"],
        created_at: "2026-03-04T00:00:00.000Z",
      },
    });

    selectedActorId = chooseActor(actorResponse.actor.id, storage);

    await client.createEvent({
      event: {
        type: "message_posted",
        refs: [],
        summary: "hello",
        provenance: { sources: ["actor_statement:test"] },
      },
    });

    const eventRequest = calls.find(
      (call) => call.url.endsWith("/events") && call.method === "POST",
    );

    expect(eventRequest).toBeTruthy();
    expect(eventRequest.body.actor_id).toBe("actor-alex");
    expect(eventRequest.body.event.summary).toBe("hello");
  });
});
