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
    expect(workMoveRequest(draft)).toEqual({
      operation: "work.move",
      request: { ref: work.ref, body: { under: parent.ref } },
    });
    expect(workMoveTitle(draft)).toBe("Move “Retries” under “Billing”");
    expect(
      workMoveRequest(workMoveDraft(work, "unsorted")).request.body,
    ).toEqual({ unsorted: true });
  });

  it("requires reasons for top level and initiative retirement", () => {
    const root = workMoveDraft(work, "root");
    expect(() => workMoveRequest(root)).toThrow("reason");
    root.reason = "Independent outcome";
    expect(workMoveRequest(root).request.body).toEqual({
      root: true,
      reason: "Independent outcome",
    });
    const retired = workMoveDraft(
      { ...work, is_initiative: true },
      "under",
      parent,
    );
    expect(() => workMoveRequest(retired)).toThrow("reason");
    retired.reason = "One shared outcome";
    expect(workMoveRequest(retired).request.body).toEqual({
      under: parent.ref,
      reason: "One shared outcome",
    });
    retired.reason = "x".repeat(2001);
    expect(() => workMoveRequest(retired)).toThrow("2000");
  });

  it("uses the move route and preserves the saved key outside the exact body", async () => {
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
    const body = { under: parent.ref };
    await client.moveWork(work.ref, body, "saved-key");
    expect(seen).toEqual([
      {
        url: "https://core.test/work/card%3Aretry/move",
        method: "POST",
        body,
        key: "saved-key",
      },
    ]);
  });
});
