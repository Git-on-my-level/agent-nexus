import { expect, it } from "vitest";
import { readFileSync } from "node:fs";
import {
  autoRetryMaxAgeMs,
  autoApplyStageKey,
  replayExpired,
  replayPolicy,
  freshRequest,
  intentFingerprint,
  randomUUID,
  requestIntent,
  sha256Hex,
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
    autoDispatchKey: "dispatch-seed",
    autoReconcileKey: "reconcile-seed",
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
  expect(fresh.autoDispatchKey).toBe("dispatch-seed");
  expect(fresh.autoReconcileKey).not.toBe("reconcile-seed");
  expect(fresh.autoReconcile.request_key).not.toBe("uncertain");
  expect(fresh.autoReconcile.if_revision).toBe(3);
  expect(requestIntent(fresh)).toEqual(requestIntent(request));
});

it("computes identical SHA-256 digests and valid v4 UUIDs when crypto.subtle and crypto.randomUUID are unavailable", async () => {
  const vectors = [
    ["", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"],
    ["abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"],
    [
      "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq",
      "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1",
    ],
  ];
  for (const [input, expected] of vectors) {
    const bytes = new TextEncoder().encode(input);
    expect(await sha256Hex(bytes, { forceFallback: true })).toBe(expected);
    if (globalThis.crypto?.subtle) {
      expect(await sha256Hex(bytes, { forceFallback: true })).toBe(
        await sha256Hex(bytes),
      );
    }
  }

  const sampleRequest = {
    body: { request_key: "k1", title: "Plain HTTP Tailnet", count: 42 },
  };
  const subtleDigest = await intentFingerprint(sampleRequest);

  const prevRandomUUID = globalThis.crypto.randomUUID;
  const prevSubtle = globalThis.crypto.subtle;
  Object.defineProperty(globalThis.crypto, "randomUUID", {
    configurable: true,
    value: undefined,
  });
  Object.defineProperty(globalThis.crypto, "subtle", {
    configurable: true,
    value: undefined,
  });
  try {
    const id = randomUUID();
    expect(id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(randomUUID()).not.toBe(id);
    expect(await intentFingerprint(sampleRequest)).toBe(subtleDigest);
  } finally {
    Object.defineProperty(globalThis.crypto, "randomUUID", {
      configurable: true,
      value: prevRandomUUID,
    });
    Object.defineProperty(globalThis.crypto, "subtle", {
      configurable: true,
      value: prevSubtle,
    });
  }
});

it("two independent copies of a legacy auto-apply item recover the same stage keys", async () => {
  const item = { key: "persisted-item-id", request: { autoApply: true } };
  const first = JSON.parse(JSON.stringify(item));
  const second = JSON.parse(JSON.stringify(item));
  const dispatch = await autoApplyStageKey(first, "autoDispatch");
  const reconcile = await autoApplyStageKey(first, "autoReconcile");
  expect(await autoApplyStageKey(second, "autoDispatch")).toBe(dispatch);
  expect(await autoApplyStageKey(second, "autoReconcile")).toBe(reconcile);
  expect(dispatch).not.toBe(reconcile);
  expect(
    await autoApplyStageKey({ ...item, key: "another-item" }, "autoDispatch"),
  ).not.toBe(dispatch);
  first.request.autoDispatchKey = "persisted-seed";
  expect(await autoApplyStageKey(first, "autoDispatch")).toBe("persisted-seed");
  first.request.autoDispatch = { request_key: "already-sent" };
  expect(await autoApplyStageKey(first, "autoDispatch")).toBe("already-sent");
  expect(item.request).toEqual({ autoApply: true });
});
