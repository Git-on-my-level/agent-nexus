import { describe, expect, it } from "vitest";
import {
  workMoveDraft,
  workMoveRequest,
  workMoveTitle,
} from "../../src/lib/workMove.js";
import { createAnxCoreClient } from "../../src/lib/anxCoreClient.js";

const work = { ref: "card:retry", title: "Retries", needs_home: true };
const parent = { ref: "card:billing", title: "Billing" };

describe("stored-parent move intents", () => {
  it("sends exactly one destination without authoring a plan", () => {
    const draft = workMoveDraft(work, "under", parent);
    expect(workMoveRequest(draft, "human", "saved-key")).toEqual({
      operation: "work.move",
      request: {
        ref: work.ref,
        body: {
          actor_id: "human",
          request_key: "saved-key",
          under: parent.ref,
        },
      },
    });
    expect(workMoveTitle(draft)).toBe("Move “Retries” under “Billing”");
    expect(
      workMoveRequest(workMoveDraft(work, "unsorted"), "human", "saved-key")
        .request.body,
    ).toEqual({ actor_id: "human", request_key: "saved-key", unsorted: true });
  });

  it("requires reasons for top level and initiative retirement", () => {
    const root = workMoveDraft(work, "root");
    expect(() => workMoveRequest(root, "human", "saved-key")).toThrow("reason");
    root.reason = "Independent outcome";
    expect(workMoveRequest(root, "human", "saved-key").request.body).toEqual({
      actor_id: "human",
      request_key: "saved-key",
      root: true,
      reason: "Independent outcome",
    });
    const retired = workMoveDraft(
      { ...work, is_initiative: true },
      "under",
      parent,
    );
    expect(() => workMoveRequest(retired, "human", "saved-key")).toThrow(
      "reason",
    );
    retired.reason = "One shared outcome";
    expect(workMoveRequest(retired, "human", "saved-key").request.body).toEqual(
      {
        actor_id: "human",
        request_key: "saved-key",
        under: parent.ref,
        reason: "One shared outcome",
      },
    );
    retired.reason = "x".repeat(2001);
    expect(() => workMoveRequest(retired, "human", "saved-key")).toThrow(
      "2000",
    );
  });

  it("uses the move route and preserves the saved replay key in the body", async () => {
    const seen = [];
    const client = createAnxCoreClient({
      baseUrl: "https://core.test",
      actorIdProvider: () => "human",
      fetchFn: async (url, init) => {
        seen.push({
          url: String(url),
          method: init.method,
          body: JSON.parse(init.body),
          key: new Headers(init.headers).get("Idempotency-Key"),
        });
        return new Response(JSON.stringify({ work: { ref: work.ref } }), {
          status: 200,
        });
      },
    });
    const body = { under: parent.ref, request_key: "saved-key" };
    await client.moveWork(work.ref, body);
    expect(seen).toEqual([
      {
        url: "https://core.test/work/card%3Aretry/move",
        method: "POST",
        body: { ...body, actor_id: "human" },
        key: null,
      },
    ]);
  });
});
