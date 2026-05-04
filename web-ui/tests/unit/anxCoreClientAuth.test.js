import { describe, expect, it } from "vitest";

import { createAnxCoreClient } from "../../src/lib/anxCoreClient.js";

describe("anxCoreClient auth behavior", () => {
  it("refreshes once on 401 responses and retries with the new bearer token", async () => {
    let accessToken = "stale-token";
    const seenAuthHeaders = [];

    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      tokenProvider: {
        getAccessToken() {
          return accessToken;
        },
        hasRefreshToken() {
          return true;
        },
        async refreshAccessToken() {
          accessToken = "fresh-token";
          return accessToken;
        },
      },
      fetchFn: async (_url, options = {}) => {
        const headers = new Headers(options.headers);
        seenAuthHeaders.push(headers.get("authorization"));

        if (headers.get("authorization") === "Bearer stale-token") {
          return new Response(
            JSON.stringify({
              error: {
                code: "invalid_token",
                message: "expired",
              },
            }),
            {
              status: 401,
              headers: { "content-type": "application/json" },
            },
          );
        }

        return new Response(JSON.stringify({ threads: [] }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      },
    });

    await expect(client.listThreads({})).resolves.toEqual({ threads: [] });
    expect(seenAuthHeaders).toEqual([
      "Bearer stale-token",
      "Bearer fresh-token",
    ]);
  });

  it("locks actor_id to the authenticated principal actor when requested", async () => {
    let capturedBody;

    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      actorIdProvider: () => "actor-principal",
      lockActorIdProvider: true,
      fetchFn: async (_url, options = {}) => {
        capturedBody = JSON.parse(options.body);
        return new Response(JSON.stringify({ event: { id: "event-1" } }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      },
    });

    await client.createEvent({
      actor_id: "actor-other",
      event: {
        type: "message_posted",
        refs: [],
        summary: "locked",
        provenance: { sources: ["event:test"] },
      },
    });

    expect(capturedBody.actor_id).toBe("actor-principal");
  });

  it("streams notification receipt updates from the receipt SSE endpoint", async () => {
    let requestedUrl = "";
    const seenReceipts = [];
    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async (url) => {
        requestedUrl = String(url);
        return new Response(
          [
            "id: receipt:wake-1@abc",
            "event: notification_receipt",
            'data: {"receipt":{"wakeup_id":"wake-1","trigger_event_id":"event-1","delivery_status":"completed"}}',
            "",
            "",
          ].join("\n"),
          {
            status: 200,
            headers: { "content-type": "text/event-stream" },
          },
        );
      },
    });

    await client.streamNotificationReceipts({
      threadId: "thread-1",
      lastEventId: "receipt:wake-1@old",
      onReceipt: (receipt, message) => {
        seenReceipts.push({ receipt, id: message.id });
      },
    });

    expect(requestedUrl).toBe(
      "http://core.test/agent-notification-receipts/stream?thread_id=thread-1&last_event_id=receipt%3Awake-1%40old",
    );
    expect(seenReceipts).toEqual([
      {
        id: "receipt:wake-1@abc",
        receipt: {
          wakeup_id: "wake-1",
          trigger_event_id: "event-1",
          delivery_status: "completed",
        },
      },
    ]);
  });
});
