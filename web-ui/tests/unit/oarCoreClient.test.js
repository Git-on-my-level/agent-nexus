import { describe, expect, it } from "vitest";

import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";
import {
  createOarCoreClient,
  verifyCoreSchemaVersion,
} from "../../src/lib/oarCoreClient.js";

describe("oarCoreClient error messaging", () => {
  it("forwards actor list query parameters", async () => {
    const seenUrls = [];
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async (url) => {
        seenUrls.push(String(url));
        return new Response(JSON.stringify({ actors: [] }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      },
    });

    await client.listActors({ q: "alice", limit: 7 });

    expect(seenUrls).toEqual(["http://core.test/actors?q=alice&limit=7"]);
  });

  it("returns actionable guidance when core is unreachable", async () => {
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async () => {
        throw new TypeError("fetch failed");
      },
    });

    await expect(client.listActors()).rejects.toThrow(
      /Unable to reach oar-core at http:\/\/core\.test[\s\S]*Check that oar-core is running and OAR_CORE_BASE_URL is correct\./,
    );
  });

  it("extracts nested JSON error messages from non-2xx responses", async () => {
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "core_unreachable",
              message: "backend unavailable",
            },
          }),
          {
            status: 503,
            statusText: "Service Unavailable",
            headers: { "content-type": "application/json" },
          },
        ),
    });

    await expect(client.listActors()).rejects.toThrow(
      /backend unavailable[\s\S]*oar-core may be unavailable; verify backend startup and base URL\./,
    );
  });

  it("reports raw-response failures once with command context", async () => {
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async () =>
        new Response(JSON.stringify({ error: "bad actor filter" }), {
          status: 400,
          statusText: "Bad Request",
          headers: { "content-type": "application/json" },
        }),
    });

    await expect(client.listActors()).rejects.toThrow(
      "oar-core request failed at http://core.test: GET /actors (400) - bad actor filter",
    );
  });

  it("verifies schema via handshake when available", async () => {
    const expectedDigest = await getExpectedCommandRegistryDigest();
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async (url) => {
        if (String(url).endsWith("/meta/handshake")) {
          return new Response(
            JSON.stringify({
              schema_version: "0.3.0",
              command_registry_digest: expectedDigest,
              core_version: "test",
              api_version: "0.2",
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
      },
    });

    await expect(verifyCoreSchemaVersion(client)).resolves.toMatchObject({
      schema_version: "0.3.0",
    });
  });

  it("rejects with guidance when handshake returns empty body", async () => {
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async () =>
        new Response("", {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });

    await expect(verifyCoreSchemaVersion(client)).rejects.toThrow(
      /empty response[\s\S]*Node adapter/,
    );
  });

  it("falls back to /version when handshake is unavailable", async () => {
    const expectedDigest = await getExpectedCommandRegistryDigest();
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async (url) => {
        if (String(url).endsWith("/meta/handshake")) {
          return new Response("not found", {
            status: 404,
            statusText: "Not Found",
          });
        }

        if (String(url).endsWith("/version")) {
          return new Response(
            JSON.stringify({
              schema_version: "0.3.0",
              command_registry_digest: expectedDigest,
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
      },
    });

    await expect(verifyCoreSchemaVersion(client)).resolves.toMatchObject({
      schema_version: "0.3.0",
    });
  });

  it("surfaces non-Error /version fallback failures directly", async () => {
    const client = {
      baseUrl: "http://core.test",
      async getHandshake() {
        const error = new Error("not found");
        error.status = 404;
        throw error;
      },
      async getVersion() {
        throw "version probe exploded";
      },
    };

    await expect(verifyCoreSchemaVersion(client)).rejects.toThrow(
      "Unable to verify oar-core schema version at http://core.test: version probe exploded",
    );
  });

  it("rejects when the deployed core advertises a different command registry", async () => {
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async (url) => {
        if (String(url).endsWith("/meta/handshake")) {
          return new Response(
            JSON.stringify({
              schema_version: "0.3.0",
              command_registry_digest: "stale-core-digest",
              core_version: "test",
              api_version: "0.2",
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
      },
    });

    await expect(verifyCoreSchemaVersion(client)).rejects.toThrow(
      /oar-core contract mismatch at http:\/\/core\.test[\s\S]*web UI is newer than the deployed core/i,
    );
  });

  it("consumes thread-scoped event streams", async () => {
    const events = [];
    const seenUrls = [];
    const encoder = new TextEncoder();
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async (url) => {
        seenUrls.push(String(url));
        return new Response(
          new ReadableStream({
            start(controller) {
              controller.enqueue(
                encoder.encode(
                  'id: evt-1\nevent: event\ndata: {"event":{"id":"evt-1","thread_id":"thread-1","type":"message_posted"}}\n\n',
                ),
              );
              controller.close();
            },
          }),
          {
            status: 200,
            headers: { "content-type": "text/event-stream" },
          },
        );
      },
    });

    await client.streamThreadEvents({
      threadId: "thread-1",
      onEvent: (event) => events.push(event),
    });

    expect(seenUrls).toEqual([
      "http://core.test/events/stream?thread_id=thread-1",
    ]);
    expect(events).toEqual([
      {
        id: "evt-1",
        event: "event",
        data: {
          event: {
            id: "evt-1",
            thread_id: "thread-1",
            type: "message_posted",
          },
        },
      },
    ]);
  });

  it("forwards work-order create bodies without client-side packet mutation", async () => {
    const seenBodies = [];
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      actorIdProvider: () => "actor-1",
      fetchFn: async (url, init) => {
        seenBodies.push(init?.body ?? "");
        return new Response(
          JSON.stringify({
            artifact: { id: "artifact-work-order-x" },
            packet_kind: "work_order",
            packet: {},
          }),
          {
            status: 201,
            headers: { "content-type": "application/json" },
          },
        );
      },
    });

    await client.createWorkOrder({
      request_key: "rk-1",
      artifact: { kind: "work_order" },
      packet: {
        subject_ref: "topic:thr-9",
        objective: "do it",
        constraints: ["c"],
        context_refs: ["topic:thr-9"],
        acceptance_criteria: ["a"],
        definition_of_done: ["d"],
      },
    });

    expect(seenBodies.length).toBe(1);
    const body = JSON.parse(seenBodies[0]);
    expect(body.packet.subject_ref).toBe("topic:thr-9");
  });

  it("posts inbox acknowledgements to the contract acknowledge path", async () => {
    const seenRequests = [];
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      actorIdProvider: () => "actor-1",
      fetchFn: async (url, init) => {
        seenRequests.push({
          url: String(url),
          method: init?.method ?? "GET",
          body: init?.body ?? "",
        });
        return new Response(JSON.stringify({ event: { id: "evt-ack" } }), {
          status: 201,
          headers: { "content-type": "application/json" },
        });
      },
    });

    await client.ackInboxItem({
      inbox_item_id: "inbox:decision_needed:thread-1:none:evt-1",
      subject_ref: "thread:thread-1",
    });

    expect(seenRequests).toEqual([
      {
        url: "http://core.test/inbox/inbox%3Adecision_needed%3Athread-1%3Anone%3Aevt-1/acknowledge",
        method: "POST",
        body: expect.any(String),
      },
    ]);
    expect(JSON.parse(seenRequests[0].body)).toEqual({
      actor_id: "actor-1",
      subject_ref: "thread:thread-1",
    });
  });

  it("routes card archive, restore, and purge through generated command paths", async () => {
    const seen = [];
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      actorIdProvider: () => "actor-1",
      fetchFn: async (url, init) => {
        seen.push({
          url: String(url),
          method: String(init?.method ?? "GET"),
        });
        return new Response(JSON.stringify({ card: { id: "c1" } }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      },
    });

    await client.archiveCard("card-1", {});
    await client.restoreCard("card-1", {});
    await client.purgeCard("card-1", {});

    expect(seen).toEqual([
      {
        url: "http://core.test/cards/card-1/archive",
        method: "POST",
      },
      {
        url: "http://core.test/cards/card-1/restore",
        method: "POST",
      },
      {
        url: "http://core.test/cards/card-1/purge",
        method: "POST",
      },
    ]);
  });
});
