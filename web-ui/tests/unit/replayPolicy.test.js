import { expect, it } from "vitest";
import { readFileSync } from "node:fs";
import {
  autoRetryMaxAgeMs,
  replayExpired,
  replayPolicy,
  freshRequest,
  requestIntent,
} from "../../src/lib/replayPolicy.js";

it("stops recovery at the shared cutoff, before receipt collection", () => {
  const created = 1_000_000;
  expect(replayExpired(created, created + autoRetryMaxAgeMs - 1)).toBe(false);
  expect(replayExpired(created, created + autoRetryMaxAgeMs)).toBe(true);
  expect(replayExpired(undefined)).toBe(true);
  expect(replayPolicy.autoRetryMaxAgeDays).toBeLessThan(
    replayPolicy.receiptRetentionDays,
  );
  expect(
    readFileSync(
      new URL("../../docs/durable-user-work.md", import.meta.url),
      "utf8",
    ),
  ).toContain(
    `Policy: receipt retention: ${replayPolicy.receiptRetentionDays} days; automatic recovery: ${replayPolicy.autoRetryMaxAgeDays} days.`,
  );
});

it("a deliberate new request replaces transport keys without changing content or concurrency bases", () => {
  const request = {
    body: {
      request_key: "original",
      if_updated_at: "base",
      content: { request_key: "authored data" },
    },
    creation: {
      request_key: "conversation",
      replay_key: "conversation-receipt",
    },
    message: {
      request_key: "message",
      replay_key: "message-receipt",
      text: " Keep this ",
    },
  };
  const fresh = freshRequest(request);
  expect(fresh.body.request_key).not.toBe(request.body.request_key);
  expect(fresh.creation.replay_key).not.toBe(request.creation.replay_key);
  expect(fresh.message.request_key).not.toBe(request.message.request_key);
  expect(requestIntent(fresh)).toEqual(requestIntent(request));
  expect(fresh.body.content.request_key).toBe("authored data");
  expect(request.message.request_key).toBe("message");
  const legacy = { message: { request_key: "legacy", text: "unchanged" } };
  expect(freshRequest(legacy, "pm.chat.send").message.replay_key).toEqual(
    expect.any(String),
  );
  expect(legacy.message.replay_key).toBeUndefined();
});

it("renewing an expired inbox batch keeps confirmed member keys and frozen remaining bases", () => {
  const request = {
    items: [
      { id: "first", body: { request_key: "confirmed", if_revision: 2 } },
      { id: "second", body: { request_key: "pending", if_revision: 7 } },
    ],
  };
  const fresh = freshRequest(request, "inbox.controls", {
    controls: [{ key: "confirmed" }],
  });
  expect(fresh.items[0]).toEqual(request.items[0]);
  expect(fresh.items[1].body.request_key).not.toBe("pending");
  expect(fresh.items[1].body.if_revision).toBe(7);
  expect(request.items[1].body.request_key).toBe("pending");
});

it("renewing auto-apply rotates only the unconfirmed stage and retains its revision", () => {
  const request = {
    answer: { request_key: "answered", revision: 2 },
    autoDispatch: {
      request_key: "admitted",
      if_revision: 3,
      if_action_revision: 1,
    },
    autoReconcile: { request_key: "uncertain", if_revision: 3 },
  };
  const fresh = freshRequest(request, "pm.decision.answer", {
    decision: { id: "decision" },
    dispatchedAction: { id: "action" },
  });
  expect(fresh.answer).toEqual(request.answer);
  expect(fresh.autoDispatch).toEqual(request.autoDispatch);
  expect(fresh.autoReconcile.request_key).not.toBe("uncertain");
  expect(fresh.autoReconcile.if_revision).toBe(3);
  expect(requestIntent(fresh)).toEqual(requestIntent(request));
});
