import { describe, expect, it } from "vitest";
import { webcrypto } from "node:crypto";

import {
  bridgeCheckinEventId,
  describeWakeRouting,
  registrationDocumentId,
} from "../../src/lib/wakeRouting.js";

const bridgeProofKeyPromise = (async () => {
  const keyPair = await webcrypto.subtle.generateKey(
    { name: "ECDSA", namedCurve: "P-256" },
    true,
    ["sign", "verify"],
  );
  const publicKey = await webcrypto.subtle.exportKey("spki", keyPair.publicKey);
  return {
    privateKey: keyPair.privateKey,
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

async function signCheckinPayload(content) {
  const { privateKey } = await bridgeProofKeyPromise;
  const signature = await webcrypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    privateKey,
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
  return Buffer.from(signature).toString("base64");
}

function registrationDoc(content) {
  return {
    state: "ok",
    document: {
      document: {
        id: "agentreg.m4-hermes",
        status: "active",
      },
      revision: {
        content,
      },
    },
  };
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

  it("marks an agent wakeable when the durable workspace id is bound", async () => {
    const { publicKeyB64 } = await bridgeProofKeyPromise;
    const result = await describeWakeRouting(
      principal,
      registrationDoc({
        handle: "m4-hermes",
        actor_id: "actor-ops-ai",
        status: "active",
        bridge_signing_public_key_spki_b64: publicKeyB64,
        bridge_checkin_event_id: "event-bridge-checkin-1",
        workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
      }),
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

    expect(registrationDocumentId("m4-hermes")).toBe("agentreg.m4-hermes");
    expect(
      bridgeCheckinEventId(
        registrationDoc({
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          bridge_checkin_event_id: "event-bridge-checkin-1",
        }),
      ),
    ).toBe("event-bridge-checkin-1");
    expect(result).toMatchObject({
      applicable: true,
      wakeable: true,
      badgeLabel: "Wakeable",
      summary: "Wakeable as @m4-hermes.",
    });
  });

  it("distinguishes transient registration lookup failures from missing docs", async () => {
    await expect(
      describeWakeRouting(principal, { state: "missing" }, "ws-123", null),
    ).resolves.toMatchObject({
      applicable: true,
      wakeable: false,
      badgeLabel: "Not wakeable",
      summary: "Missing registration document agentreg.m4-hermes.",
    });

    await expect(
      describeWakeRouting(principal, { state: "error" }, "ws-123", null),
    ).resolves.toMatchObject({
      applicable: true,
      wakeable: false,
      badgeLabel: "Unknown",
      summary: "Registration status is unavailable right now.",
    });

    const { publicKeyB64 } = await bridgeProofKeyPromise;
    await expect(
      describeWakeRouting(
        principal,
        registrationDoc({
          handle: "m4-hermes",
          actor_id: "actor-ops-ai",
          status: "active",
          bridge_signing_public_key_spki_b64: publicKeyB64,
          bridge_checkin_event_id: "event-bridge-checkin-1",
          workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
        }),
        "ws-123",
        { state: "error" },
      ),
    ).resolves.toMatchObject({
      applicable: true,
      wakeable: false,
      badgeLabel: "Unknown",
      summary: "Bridge check-in status is unavailable right now.",
    });
  });

  it("treats missing durable workspace ids as indeterminate", async () => {
    const { publicKeyB64 } = await bridgeProofKeyPromise;
    const result = await describeWakeRouting(
      principal,
      registrationDoc({
        handle: "m4-hermes",
        actor_id: "actor-ops-ai",
        status: "active",
        bridge_signing_public_key_spki_b64: publicKeyB64,
        bridge_checkin_event_id: "event-bridge-checkin-1",
        workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
      }),
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
      wakeable: false,
      badgeLabel: "Unknown",
      summary:
        "Workspace binding status is unavailable because this workspace has no durable workspace ID.",
    });
  });

  it("keeps pending registrations non-wakeable until the bridge checks in", async () => {
    const result = await describeWakeRouting(
      principal,
      registrationDoc({
        handle: "m4-hermes",
        actor_id: "actor-ops-ai",
        status: "pending",
        workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
      }),
      "ws-123",
      null,
    );

    expect(result).toMatchObject({
      applicable: true,
      wakeable: false,
      badgeLabel: "Not wakeable",
      summary:
        "Bridge has not checked in yet. Start the bridge before humans tag this agent.",
    });
  });

  it("treats stale bridge check-ins as not wakeable", async () => {
    const { publicKeyB64 } = await bridgeProofKeyPromise;
    const result = await describeWakeRouting(
      principal,
      registrationDoc({
        handle: "m4-hermes",
        actor_id: "actor-ops-ai",
        status: "active",
        bridge_signing_public_key_spki_b64: publicKeyB64,
        bridge_checkin_event_id: "event-bridge-checkin-1",
        workspace_bindings: [{ workspace_id: "ws-123", enabled: true }],
      }),
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
      wakeable: false,
      badgeLabel: "Not wakeable",
      summary: "Bridge check-in is stale. Restart or reconnect the bridge.",
    });
  });
});
