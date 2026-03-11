import { describe, expect, it } from "vitest";

import {
  createOarCoreClient,
  verifyCoreSchemaVersion,
} from "../../src/lib/oarCoreClient.js";

describe("oarCoreClient error messaging", () => {
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

  it("verifies schema via handshake when available", async () => {
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async (url) => {
        if (String(url).endsWith("/meta/handshake")) {
          return new Response(
            JSON.stringify({
              schema_version: "0.2.2",
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
      schema_version: "0.2.2",
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
          return new Response(JSON.stringify({ schema_version: "0.2.2" }), {
            status: 200,
            headers: { "content-type": "application/json" },
          });
        }

        return new Response("not found", {
          status: 404,
          statusText: "Not Found",
        });
      },
    });

    await expect(verifyCoreSchemaVersion(client)).resolves.toMatchObject({
      schema_version: "0.2.2",
    });
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
});
