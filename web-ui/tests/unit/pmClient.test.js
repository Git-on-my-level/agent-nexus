import { describe, expect, it } from "vitest";
import { createAnxCoreClient } from "../../src/lib/anxCoreClient.js";

describe("canonical work and PM client", () => {
  function setup(status = 200, response = {}) {
    const requests = [];
    const client = createAnxCoreClient({
      baseUrl: "http://core.test",
      actorIdProvider: () => "human",
      lockActorIdProvider: () => true,
      requestContextHeadersProvider: () => ({ "x-anx-workspace": "local" }),
      fetchFn: async (url, init) => {
        requests.push({ url: String(url), init });
        return new Response(JSON.stringify(response), {
          status,
          headers: { "content-type": "application/json" },
        });
      },
    });
    return { client, requests };
  }
  it("uses the canonical work filters and opaque cursor", async () => {
    const { client, requests } = setup(200, { work: [], next_cursor: null });
    await client.listWork({
      source: "github",
      phase: "blocked",
      cursor: "opaque+/=",
      limit: 50,
    });
    const url = new URL(requests[0].url);
    expect(url.pathname).toBe("/work");
    expect(url.searchParams.get("cursor")).toBe("opaque+/=");
    expect(url.searchParams.get("source")).toBe("github");
    expect(requests[0].init.method).toBe("GET");
  });
  it("encodes work references and keeps refresh queued rather than synthesizing evidence", async () => {
    const { client, requests } = setup(202, { refresh: { state: "queued" } });
    expect(await client.requestWorkRefresh("card:sample")).toEqual({
      refresh: { state: "queued" },
    });
    expect(new URL(requests[0].url).pathname).toBe(
      "/work/card%3Asample/refresh",
    );
    expect(requests[0].init.method).toBe("POST");
  });
  it("posts PM messages with a stable request key and no selected actor claim", async () => {
    const { client, requests } = setup(202, {
      id: "turn-1",
      status: "sending",
    });
    await client.sendPmMessage("conversation-1", {
      request_key: "request-1",
      text: "What changed?",
    });
    expect(new URL(requests[0].url).pathname).toBe(
      "/pm/conversations/conversation-1/messages",
    );
    expect(JSON.parse(requests[0].init.body)).toEqual({
      request_key: "request-1",
      text: "What changed?",
    });
  });
  it("preserves decision revision and does not dispatch when answering", async () => {
    const { client, requests } = setup();
    await client.answerPmDecision("decision-1", {
      revision: 3,
      approve: true,
      text: "Proceed within this scope",
    });
    expect(requests).toHaveLength(1);
    expect(new URL(requests[0].url).pathname).toBe(
      "/pm/decisions/decision-1/answer",
    );
    expect(JSON.parse(requests[0].init.body).revision).toBe(3);
  });
  it("surfaces source conflicts and provider unavailability as failures", async () => {
    const { client } = setup(409, {
      error: {
        code: "source_revision_changed",
        message: "Source revision changed",
      },
    });
    await expect(client.dispatchPmDecision("decision-1")).rejects.toMatchObject(
      { status: 409 },
    );
  });
});
