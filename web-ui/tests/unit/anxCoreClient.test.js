import { describe, expect, it } from "vitest";

import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";
import {
  createAnxCoreClient,
  verifyCoreSchemaVersion,
} from "../../src/lib/anxCoreClient.js";
import { buildTopicCreatePayloadFromDraft } from "../../src/lib/topicCreatePayload.js";

describe("anxCoreClient error messaging", () => {
  it("sends empty actor_id for writes when session locks identity and provider is empty", async () => {
    const seenBodies = [];
    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      actorIdProvider: () => "",
      lockActorIdProvider: () => true,
      fetchFn: async (url, init) => {
        seenBodies.push(init?.body ?? "");
        return new Response(JSON.stringify({ topic: { id: "t-1" } }), {
          status: 201,
          headers: { "content-type": "application/json" },
        });
      },
    });

    await client.createTopic(
      buildTopicCreatePayloadFromDraft({
        title: "x",
        summary: "",
        status: "active",
      }),
    );

    expect(seenBodies.length).toBe(1);
    expect(JSON.parse(seenBodies[0])).toMatchObject({ actor_id: "" });
  });

  it("forwards actor list query parameters", async () => {
    const seenUrls = [];
    const client = createAnxCoreClient({
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
    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async () => {
        throw new TypeError("fetch failed");
      },
    });

    await expect(client.listActors()).rejects.toThrow(
      /Unable to reach anx-core at http:\/\/core\.test[\s\S]*Check that anx-core is running and ANX_CORE_BASE_URL is correct\./,
    );
  });

  it("extracts nested JSON error messages from non-2xx responses", async () => {
    const client = createAnxCoreClient({
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
      /backend unavailable[\s\S]*anx-core may be unavailable; verify backend startup and base URL\./,
    );
  });

  it("reports raw-response failures once with command context", async () => {
    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async () =>
        new Response(JSON.stringify({ error: "bad actor filter" }), {
          status: 400,
          statusText: "Bad Request",
          headers: { "content-type": "application/json" },
        }),
    });

    await expect(client.listActors()).rejects.toThrow(
      "anx-core request failed at http://core.test: GET /actors (400) - bad actor filter",
    );
  });

  it("verifies schema via handshake when available", async () => {
    const expectedDigest = await getExpectedCommandRegistryDigest();
    const client = createAnxCoreClient({
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
    const client = createAnxCoreClient({
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
    const client = createAnxCoreClient({
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
      "Unable to verify anx-core schema version at http://core.test: version probe exploded",
    );
  });

  it("rejects when the deployed core advertises a different command registry", async () => {
    const client = createAnxCoreClient({
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
      /anx-core contract mismatch at http:\/\/core\.test[\s\S]*web UI is newer than the deployed core/i,
    );
  });

  it("consumes thread-scoped event streams", async () => {
    const events = [];
    const seenUrls = [];
    const encoder = new TextEncoder();
    const client = createAnxCoreClient({
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

  it("forwards receipt create bodies without mutating packet", async () => {
    const seenBodies = [];
    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      actorIdProvider: () => "actor-1",
      fetchFn: async (url, init) => {
        seenBodies.push(init?.body ?? "");
        return new Response(
          JSON.stringify({
            artifact: { id: "artifact-receipt-x" },
            packet_kind: "receipt",
            packet: {},
          }),
          {
            status: 201,
            headers: { "content-type": "application/json" },
          },
        );
      },
    });

    await client.createReceipt({
      request_key: "rk-1",
      artifact: { kind: "receipt" },
      packet: {
        thread_id: "thr-9",
        subject_ref: "card:c-1",
        outputs: ["artifact:o1"],
        verification_evidence: ["event:e1"],
        changes_summary: "done",
        known_gaps: [],
      },
    });

    expect(seenBodies.length).toBe(1);
    const body = JSON.parse(seenBodies[0]);
    expect(body.packet.subject_ref).toBe("card:c-1");
    expect(body.packet.thread_id).toBe("thr-9");
  });

  it("posts inbox acknowledgements to the contract acknowledge path", async () => {
    const seenRequests = [];
    const client = createAnxCoreClient({
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
      inbox_item_id: "inbox:action_needed:thread-1:none:evt-1",
      subject_ref: "thread:thread-1",
    });

    expect(seenRequests).toEqual([
      {
        url: "http://core.test/inbox/inbox%3Aaction_needed%3Athread-1%3Anone%3Aevt-1/acknowledge",
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
    const client = createAnxCoreClient({
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

  it("respondInboxAsk POSTs /inbox/{id}/respond with actor_id and snake_case fields", async () => {
    const requests = [];
    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      actorIdProvider: () => "actor-1",
      fetchFn: async (url, init) => {
        requests.push({ url: String(url), init });
        return new Response(
          JSON.stringify({ event: { id: "e1" }, acknowledgment: { id: "e2" } }),
          {
            status: 201,
            headers: { "content-type": "application/json" },
          },
        );
      },
    });

    const out = await client.respondInboxAsk("ask-item-9", {
      answer: "Ship it.",
      save_as_decision: true,
      notify_asking_agent: false,
    });

    expect(out.event?.id).toBe("e1");
    expect(requests.length).toBe(1);
    expect(requests[0].url).toBe("http://core.test/inbox/ask-item-9/respond");
    expect(requests[0].init.method).toBe("POST");
    expect(JSON.parse(requests[0].init.body)).toEqual({
      actor_id: "actor-1",
      answer: "Ship it.",
      save_as_decision: true,
      notify_asking_agent: false,
    });
  });
});
