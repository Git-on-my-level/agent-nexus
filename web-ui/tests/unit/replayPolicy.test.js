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
