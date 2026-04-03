import { describe, expect, it } from "vitest";
import { createSign, generateKeyPairSync } from "node:crypto";

import {
  bridgeCheckinEventId,
  describeWakeRouting,
} from "../../src/lib/wakeRouting.js";

const bridgeProofKey = (() => {
  const { publicKey, privateKey } = generateKeyPairSync("ec", {
    namedCurve: "prime256v1",
    publicKeyEncoding: { format: "der", type: "spki" },
    privateKeyEncoding: { format: "der", type: "pkcs8" },
  });
  return {
    privateKey,
    publicKeyB64: Buffer.from(publicKey).toString("base64"),
  };
})();

function stableJsonValue(value) {
  if (Array.isArray(value)) {
    return value.map((item) => stableJsonValue(item));
  }
  if (value && typeof value === "object") {
    return Object.keys(value)
      .sort()
      .reduce((normalized, key) => {
        normalized[key] = stableJsonValue(value[key]);
        return normalized;
      }, {});
  }
  return value;
}

function signCheckinPayload(content) {
  const signer = createSign("sha256");
  signer.update(
    Buffer.from(
      JSON.stringify(
        stableJsonValue({
          v: "agent-bridge-checkin-proof/v1",
          handle: String(content.handle ?? "").trim(),
          actor_id: String(content.actor_id ?? "").trim(),
          workspace_id: String(content.workspace_id ?? "").trim(),
          bridge_instance_id: String(content.bridge_instance_id ?? "").trim(),
          checked_in_at: String(content.checked_in_at ?? "").trim(),
          expires_at: String(content.expires_at ?? "").trim(),
        }),
      ),
      "utf8",
    ),
  );
  signer.end();
  return signer
    .sign({ key: bridgeProofKey.privateKey, format: "der", type: "pkcs8" })
    .toString("base64");
}

async function checkinEvent(content) {
  return {
    state: "ok",
    document: {
      event: {
        id: "event-bridge-checkin-1",
        type: "agent_bridge_checked_in",
        payload: {
          ...content,
          proof_signature_b64: await signCheckinPayload(content),
        },
      },
    },
  };
}

describe("wakeRouting", () => {
  const principal = {
    principal_kind: "agent",
    actor_id: "actor-ops-ai",
    username: "m4-hermes",
    revoked: false,
  };

  it("marks an agent online when the durable workspace id is bound", async () => {
    const { publicKeyB64 } = bridgeProofKey;
    const result = await describeWakeRouting(
      {
        ...principal,
        registration: {
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          status: "active",
          bridge_signing_public_key_spki_b64: publicKeyB64,
          bridge_checkin_event_id: "event-bridge-checkin-1",
          workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
        },
      },
      null,
      "ws-123",
      await checkinEvent({
        handle: "m4-hermes",
        actor_id: "actor-ops-ai",
        workspace_id: "ws-123",
        bridge_instance_id: "bridge-hermes-1",
        checked_in_at: "2099-03-01T10:00:00Z",
        expires_at: "2099-03-01T10:05:00Z",
      }),
    );

    expect(
      bridgeCheckinEventId({
        ...principal,
        registration: {
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          bridge_checkin_event_id: "event-bridge-checkin-1",
        },
      }),
    ).toBe("event-bridge-checkin-1");
    expect(result).toMatchObject({
      applicable: true,
      taggable: true,
      online: true,
      offline: false,
      state: "online",
      badgeLabel: "Online",
      summary: "Online as @m4-hermes.",
    });
  });

  it("distinguishes transient registration lookup failures from missing docs", async () => {
    await expect(
      describeWakeRouting(principal, { state: "missing" }, "ws-123", null),
    ).resolves.toMatchObject({
      applicable: true,
      taggable: false,
      online: false,
      offline: false,
      state: "unregistered",
      badgeLabel: "Unregistered",
      summary: "Missing wake registration for @m4-hermes.",
    });

    await expect(
      describeWakeRouting(principal, { state: "error" }, "ws-123", null),
    ).resolves.toMatchObject({
      applicable: true,
      taggable: false,
      online: false,
      offline: false,
      state: "unknown",
      badgeLabel: "Unknown",
      summary: "Registration status is unavailable right now.",
    });

    const { publicKeyB64 } = bridgeProofKey;
    await expect(
      describeWakeRouting(
        {
          ...principal,
          registration: {
            handle: "m4-hermes",
            actor_id: "actor-ops-ai",
            status: "active",
            bridge_signing_public_key_spki_b64: publicKeyB64,
            bridge_checkin_event_id: "event-bridge-checkin-1",
            workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
          },
        },
        null,
        "ws-123",
        { state: "error" },
      ),
    ).resolves.toMatchObject({
      applicable: true,
      taggable: true,
      online: false,
      offline: true,
      state: "unknown",
      badgeLabel: "Unknown",
      summary: "Bridge check-in status is unavailable right now.",
    });
  });

  it("keeps healthy bridge registrations online when page workspace context is missing", async () => {
    const { publicKeyB64 } = bridgeProofKey;
    const result = await describeWakeRouting(
      {
        ...principal,
        registration: {
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          status: "active",
          bridge_signing_public_key_spki_b64: publicKeyB64,
          bridge_checkin_event_id: "event-bridge-checkin-1",
          workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
        },
      },
      null,
      "",
      await checkinEvent({
        handle: "m4-hermes",
        actor_id: "actor-ops-ai",
        workspace_id: "ws-123",
        bridge_instance_id: "bridge-hermes-1",
        checked_in_at: "2099-03-01T10:00:00Z",
        expires_at: "2099-03-01T10:05:00Z",
      }),
    );

    expect(result).toMatchObject({
      applicable: true,
      taggable: true,
      online: true,
      offline: false,
      state: "online",
      badgeLabel: "Online",
      summary:
        "Online for bound workspace ws-123, but this page has no durable workspace ID to confirm the current workspace match.",
    });
  });

  it("treats a fresh principal registration heartbeat as online when no check-in event id is present", async () => {
    const { publicKeyB64 } = bridgeProofKey;
    const result = await describeWakeRouting(
      {
        ...principal,
        registration: {
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          status: "active",
          bridge_signing_public_key_spki_b64: publicKeyB64,
          bridge_instance_id: "bridge-hermes-1",
          bridge_checked_in_at: "2099-03-01T10:00:00Z",
          bridge_expires_at: "2099-03-01T10:05:00Z",
          workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
        },
      },
      null,
      "ws-123",
      null,
    );

    expect(result).toMatchObject({
      applicable: true,
      taggable: true,
      online: true,
      offline: false,
      state: "online",
      badgeLabel: "Online",
      summary: "Online as @m4-hermes.",
    });
  });

  it("keeps registered agents taggable but offline until the bridge checks in", async () => {
    const { publicKeyB64 } = bridgeProofKey;
    const result = await describeWakeRouting(
      {
        ...principal,
        registration: {
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          status: "pending",
          bridge_signing_public_key_spki_b64: publicKeyB64,
          workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
        },
      },
      null,
      "ws-123",
      null,
    );

    expect(result).toMatchObject({
      applicable: true,
      taggable: true,
      online: false,
      offline: true,
      state: "offline",
      badgeLabel: "Offline",
      summary:
        "Offline. The agent is registered for this workspace, but no fresh bridge check-in is available yet.",
    });
  });

  it("keeps agents online when the check-in event lookup is missing but the principal heartbeat is still fresh", async () => {
    const { publicKeyB64 } = bridgeProofKey;
    const result = await describeWakeRouting(
      {
        ...principal,
        registration: {
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          status: "active",
          bridge_signing_public_key_spki_b64: publicKeyB64,
          bridge_checkin_event_id: "event-bridge-checkin-1",
          bridge_instance_id: "bridge-hermes-1",
          bridge_checked_in_at: "2099-03-01T10:00:00Z",
          bridge_expires_at: "2099-03-01T10:05:00Z",
          workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
        },
      },
      null,
      "ws-123",
      { state: "missing" },
    );

    expect(result).toMatchObject({
      applicable: true,
      taggable: true,
      online: true,
      offline: false,
      state: "online",
      badgeLabel: "Online",
      summary: "Online as @m4-hermes.",
    });
  });

  it("treats stale bridge check-ins as offline", async () => {
    const { publicKeyB64 } = bridgeProofKey;
    const result = await describeWakeRouting(
      {
        ...principal,
        registration: {
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          status: "active",
          bridge_signing_public_key_spki_b64: publicKeyB64,
          bridge_checkin_event_id: "event-bridge-checkin-1",
          workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
        },
      },
      null,
      "ws-123",
      await checkinEvent({
        handle: "m4-hermes",
        actor_id: "actor-ops-ai",
        workspace_id: "ws-123",
        bridge_instance_id: "bridge-hermes-1",
        checked_in_at: "2026-03-01T10:00:00Z",
        expires_at: "2026-03-01T10:05:00Z",
      }),
    );

    expect(result).toMatchObject({
      applicable: true,
      taggable: true,
      online: false,
      offline: true,
      state: "offline",
      badgeLabel: "Offline",
      summary:
        "Offline. The agent is registered for this workspace, but its last bridge check-in is stale.",
    });
  });
});
