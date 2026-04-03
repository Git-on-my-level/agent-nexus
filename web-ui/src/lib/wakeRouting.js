function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value
    : null;
}

function documentContent(documentPayload) {
  const revision = asObject(asObject(documentPayload)?.revision);
  return asObject(revision?.content);
}

function eventRecord(eventPayload) {
  return asObject(asObject(eventPayload)?.event);
}

function eventPayloadContent(eventPayload) {
  return asObject(eventRecord(eventPayload)?.payload);
}

function registrationLookup(value) {
  const lookup = asObject(value);
  if (
    lookup?.state === "ok" ||
    lookup?.state === "missing" ||
    lookup?.state === "error"
  ) {
    return lookup;
  }
  return { state: "ok", document: value };
}

function parseTimestamp(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return null;
  const parsed = Date.parse(raw);
  return Number.isNaN(parsed) ? null : parsed;
}

function registrationHeartbeat(registration) {
  return {
    bridgeInstanceId: String(registration?.bridge_instance_id ?? "").trim(),
    checkedInAt: parseTimestamp(registration?.bridge_checked_in_at),
    expiresAt: parseTimestamp(registration?.bridge_expires_at),
  };
}

function hasFreshRegistrationHeartbeat(registration) {
  const heartbeat = registrationHeartbeat(registration);
  return Boolean(
    heartbeat.bridgeInstanceId &&
    heartbeat.checkedInAt &&
    heartbeat.expiresAt &&
    heartbeat.expiresAt >= Date.now(),
  );
}

function onlineWakeRouting(handle, summary) {
  return {
    applicable: true,
    handle,
    taggable: true,
    online: true,
    offline: false,
    state: "online",
    badgeLabel: "Online",
    badgeClass: "bg-emerald-500/10 text-emerald-400",
    summary,
  };
}

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

function bridgeProofMessage(checkinContent) {
  return JSON.stringify(
    stableJsonValue({
      v: "agent-bridge-checkin-proof/v1",
      handle: String(checkinContent?.handle ?? "").trim(),
      actor_id: String(checkinContent?.actor_id ?? "").trim(),
      workspace_id: String(checkinContent?.workspace_id ?? "").trim(),
      bridge_instance_id: String(
        checkinContent?.bridge_instance_id ?? "",
      ).trim(),
      checked_in_at: String(checkinContent?.checked_in_at ?? "").trim(),
      expires_at: String(checkinContent?.expires_at ?? "").trim(),
    }),
  );
}

function base64ToBytes(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return null;
  if (typeof Buffer !== "undefined") {
    return Uint8Array.from(Buffer.from(raw, "base64"));
  }
  if (typeof atob === "function") {
    return Uint8Array.from(atob(raw), (char) => char.charCodeAt(0));
  }
  return null;
}

function readDerLength(bytes, offset) {
  const first = bytes[offset];
  if (first == null) return null;
  if ((first & 0x80) === 0) {
    return { length: first, nextOffset: offset + 1 };
  }
  const byteCount = first & 0x7f;
  if (byteCount === 0 || byteCount > 4) {
    return null;
  }
  let length = 0;
  for (let index = 0; index < byteCount; index += 1) {
    const value = bytes[offset + 1 + index];
    if (value == null) return null;
    length = (length << 8) | value;
  }
  return { length, nextOffset: offset + 1 + byteCount };
}

function decodeDerInteger(bytes, offset, size) {
  if (bytes[offset] !== 0x02) {
    return null;
  }
  const parsedLength = readDerLength(bytes, offset + 1);
  if (!parsedLength) return null;
  const { length, nextOffset } = parsedLength;
  const endOffset = nextOffset + length;
  if (endOffset > bytes.length) {
    return null;
  }
  let value = bytes.slice(nextOffset, endOffset);
  while (value.length > 0 && value[0] === 0x00) {
    value = value.slice(1);
  }
  if (value.length > size) {
    return null;
  }
  const normalized = new Uint8Array(size);
  normalized.set(value, size - value.length);
  return { value: normalized, nextOffset: endOffset };
}

function derSignatureToP1363(signatureBytes, size = 32) {
  if (!(signatureBytes instanceof Uint8Array)) {
    return null;
  }
  if (signatureBytes.length === size * 2) {
    return signatureBytes;
  }
  if (signatureBytes[0] !== 0x30) {
    return null;
  }
  const parsedLength = readDerLength(signatureBytes, 1);
  if (!parsedLength) return null;
  const { length, nextOffset } = parsedLength;
  if (nextOffset + length !== signatureBytes.length) {
    return null;
  }
  const r = decodeDerInteger(signatureBytes, nextOffset, size);
  if (!r) return null;
  const s = decodeDerInteger(signatureBytes, r.nextOffset, size);
  if (!s || s.nextOffset !== signatureBytes.length) {
    return null;
  }
  const normalized = new Uint8Array(size * 2);
  normalized.set(r.value, 0);
  normalized.set(s.value, size);
  return normalized;
}

async function verifyBridgeProof(publicKeyB64, checkinContent) {
  const subtle = globalThis?.crypto?.subtle;
  const publicKeyBytes = base64ToBytes(publicKeyB64);
  const signatureBytes = base64ToBytes(checkinContent?.proof_signature_b64);
  if (!subtle || !publicKeyBytes || !signatureBytes) {
    return false;
  }
  try {
    const publicKey = await subtle.importKey(
      "spki",
      publicKeyBytes,
      { name: "ECDSA", namedCurve: "P-256" },
      false,
      ["verify"],
    );
    const messageBytes = new TextEncoder().encode(
      bridgeProofMessage(checkinContent),
    );
    if (
      await subtle.verify(
        { name: "ECDSA", hash: "SHA-256" },
        publicKey,
        signatureBytes,
        messageBytes,
      )
    ) {
      return true;
    }
    const normalizedSignature = derSignatureToP1363(signatureBytes);
    if (!normalizedSignature || normalizedSignature === signatureBytes) {
      return false;
    }
    return subtle.verify(
      { name: "ECDSA", hash: "SHA-256" },
      publicKey,
      normalizedSignature,
      messageBytes,
    );
  } catch {
    return false;
  }
}

function principalRegistration(principal, registrationDoc = null) {
  const registration = asObject(principal?.registration);
  if (registration) {
    return registration;
  }
  return documentContent(
    asObject(registrationLookup(registrationDoc))?.document,
  );
}

export function bridgeCheckinEventId(principal, registrationDoc = null) {
  const registration = principalRegistration(principal, registrationDoc);
  return String(registration?.bridge_checkin_event_id ?? "").trim();
}

export async function describeWakeRouting(
  principal,
  registrationDoc,
  workspaceId,
  bridgeCheckinDoc,
) {
  const kind = String(principal?.principal_kind ?? "").trim();
  if (kind !== "agent") {
    return { applicable: false };
  }

  const handle = String(principal?.username ?? "").trim();
  const actorId = String(principal?.actor_id ?? "").trim();
  const bindingTarget = String(workspaceId ?? "").trim();
  const lookup = registrationLookup(registrationDoc);
  const content = principalRegistration(principal, registrationDoc);

  const base = {
    applicable: true,
    handle,
    taggable: false,
    online: false,
    offline: false,
    state: "offline",
    badgeLabel: "Offline",
    badgeClass: "bg-amber-500/10 text-amber-400",
    summary: "",
  };

  if (principal?.revoked) {
    return {
      ...base,
      state: "revoked",
      badgeLabel: "Revoked",
      badgeClass: "bg-red-500/10 text-red-400",
      summary: "Revoked agent principals cannot be tagged.",
    };
  }
  if (!handle) {
    return {
      ...base,
      state: "unknown",
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: "No username is set for `@handle` routing.",
    };
  }

  if (lookup.state === "error") {
    return {
      ...base,
      state: "unknown",
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: "Registration status is unavailable right now.",
    };
  }

  if (lookup.state === "missing") {
    return {
      ...base,
      state: "unregistered",
      badgeLabel: "Unregistered",
      summary: `Missing wake registration for @${handle}.`,
    };
  }
  if (!content) {
    return {
      ...base,
      state: "unregistered",
      badgeLabel: "Unregistered",
      summary: `Missing wake registration for @${handle}.`,
    };
  }

  const registeredHandle = String(content.handle ?? "").trim();
  if (registeredHandle && registeredHandle !== handle) {
    return {
      ...base,
      state: "unknown",
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: `Wake registration handle does not match @${handle}.`,
    };
  }

  const registeredActorId = String(content.actor_id ?? "").trim();
  if (!registeredActorId || registeredActorId !== actorId) {
    return {
      ...base,
      state: "unknown",
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: "Registration actor does not match the principal actor.",
    };
  }

  const status = String(content.status ?? "active").trim() || "active";
  if (status === "disabled") {
    return {
      ...base,
      state: "disabled",
      badgeLabel: "Disabled",
      summary: "Registration is disabled.",
    };
  }

  const bindings = Array.isArray(content.workspace_bindings)
    ? content.workspace_bindings
    : [];
  const enabledBindings = bindings.filter((binding) => {
    const workspaceId = String(binding?.workspace_id ?? "").trim();
    return workspaceId && binding?.enabled !== false;
  });

  if (!bindingTarget && enabledBindings.length === 0) {
    return {
      ...base,
      state: "unregistered",
      badgeLabel: "Unregistered",
      summary: "Registration is not enabled for any workspace.",
    };
  }

  const matchingBinding = bindings.find(
    (binding) => String(binding?.workspace_id ?? "").trim() === bindingTarget,
  );

  if (bindingTarget && !matchingBinding) {
    return {
      ...base,
      state: "unregistered",
      badgeLabel: "Unregistered",
      summary: "Registration is not enabled for this workspace.",
    };
  }

  if (bindingTarget && matchingBinding.enabled === false) {
    return {
      ...base,
      state: "disabled",
      badgeLabel: "Disabled",
      summary: "Registration is disabled for this workspace.",
    };
  }

  const offline = {
    ...base,
    taggable: true,
    offline: true,
  };
  const freshRegistrationHeartbeat = hasFreshRegistrationHeartbeat(content);

  const bridgeProofKey = String(
    content.bridge_signing_public_key_spki_b64 ?? "",
  ).trim();
  if (!bridgeProofKey) {
    return {
      ...offline,
      summary:
        "Offline. The agent is registered for this workspace, but its bridge proof key is missing.",
    };
  }

  const checkinEventId = String(content.bridge_checkin_event_id ?? "").trim();
  if (!checkinEventId) {
    if (freshRegistrationHeartbeat) {
      return onlineWakeRouting(handle, `Online as @${handle}.`);
    }
    return {
      ...offline,
      summary:
        "Offline. The agent is registered for this workspace, but no fresh bridge check-in is available yet.",
    };
  }

  const checkinLookup = registrationLookup(bridgeCheckinDoc);
  if (checkinLookup.state === "error") {
    return {
      ...offline,
      state: "unknown",
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: "Bridge check-in status is unavailable right now.",
    };
  }
  if (checkinLookup.state === "missing") {
    if (freshRegistrationHeartbeat) {
      return onlineWakeRouting(handle, `Online as @${handle}.`);
    }
    return {
      ...offline,
      summary: `Offline. Missing bridge check-in event ${checkinEventId}.`,
    };
  }

  const checkin = eventRecord(checkinLookup.document);
  if (String(checkin?.type ?? "").trim() !== "agent_bridge_checked_in") {
    return {
      ...offline,
      summary: `Bridge check-in event ${checkinEventId} is invalid.`,
    };
  }

  const checkinContent = eventPayloadContent(checkinLookup.document);
  if (!checkinContent) {
    return {
      ...offline,
      summary: `Bridge check-in event ${checkinEventId} is invalid.`,
    };
  }

  const checkinHandle = String(checkinContent.handle ?? "").trim();
  if (checkinHandle && checkinHandle !== handle) {
    return {
      ...offline,
      summary: `Bridge check-in handle does not match @${handle}.`,
    };
  }

  const checkinActorId = String(checkinContent.actor_id ?? "").trim();
  if (!checkinActorId || checkinActorId !== actorId) {
    return {
      ...offline,
      summary: "Bridge check-in actor does not match the principal actor.",
    };
  }

  const checkinWorkspaceId = String(checkinContent.workspace_id ?? "").trim();
  if (bindingTarget && checkinWorkspaceId !== bindingTarget) {
    return {
      ...offline,
      summary: "Bridge check-in is for a different workspace.",
    };
  }
  if (
    !bindingTarget &&
    !enabledBindings.some(
      (binding) =>
        String(binding?.workspace_id ?? "").trim() === checkinWorkspaceId,
    )
  ) {
    return {
      ...offline,
      summary: "Bridge check-in is for an unbound workspace.",
    };
  }

  const bridgeInstanceId = String(
    checkinContent.bridge_instance_id ?? "",
  ).trim();
  if (!bridgeInstanceId) {
    return {
      ...offline,
      summary:
        "Bridge instance identity is missing. Let the live bridge rewrite this registration.",
    };
  }
  if (!(await verifyBridgeProof(bridgeProofKey, checkinContent))) {
    return {
      ...offline,
      summary: "Bridge readiness proof is invalid.",
    };
  }

  const checkedInAt = parseTimestamp(checkinContent.checked_in_at);
  const expiresAt = parseTimestamp(checkinContent.expires_at);
  if (!checkedInAt) {
    return {
      ...offline,
      summary:
        "Offline. The agent is registered for this workspace, but no fresh bridge check-in is available yet.",
    };
  }
  if (!expiresAt) {
    return {
      ...offline,
      state: "unknown",
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: "Bridge check-in metadata is incomplete right now.",
    };
  }
  if (expiresAt < Date.now()) {
    return {
      ...offline,
      summary:
        "Offline. The agent is registered for this workspace, but its last bridge check-in is stale.",
    };
  }

  if (!bindingTarget) {
    return onlineWakeRouting(
      handle,
      `Online for bound workspace ${checkinWorkspaceId}, but this page has no durable workspace ID to confirm the current workspace match.`,
    );
  }

  return onlineWakeRouting(handle, `Online as @${handle}.`);
}
