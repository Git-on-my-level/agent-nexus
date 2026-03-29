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
    return subtle.verify(
      { name: "ECDSA", hash: "SHA-256" },
      publicKey,
      signatureBytes,
      new TextEncoder().encode(bridgeProofMessage(checkinContent)),
    );
  } catch {
    return false;
  }
}

export function registrationDocumentId(handle) {
  const normalized = String(handle ?? "").trim();
  return normalized ? `agentreg.${normalized}` : "";
}

export function bridgeCheckinEventId(registrationDoc) {
  const content = documentContent(
    asObject(registrationLookup(registrationDoc))?.document,
  );
  return String(content?.bridge_checkin_event_id ?? "").trim();
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

  const base = {
    applicable: true,
    handle,
    wakeable: false,
    badgeLabel: "Not wakeable",
    badgeClass: "bg-amber-500/10 text-amber-400",
    summary: "",
  };

  if (principal?.revoked) {
    return { ...base, summary: "Revoked agent principals are not wakeable." };
  }
  if (!handle) {
    return { ...base, summary: "No username is set for `@handle` routing." };
  }

  if (lookup.state === "error") {
    return {
      ...base,
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: "Registration status is unavailable right now.",
    };
  }

  if (lookup.state === "missing") {
    return {
      ...base,
      summary: `Missing registration document ${registrationDocumentId(handle)}.`,
    };
  }

  const documentMeta = asObject(asObject(lookup.document)?.document);
  if (
    String(documentMeta?.status ?? "").trim() === "tombstoned" ||
    String(documentMeta?.tombstoned_at ?? "").trim() !== ""
  ) {
    return {
      ...base,
      summary: "Registration document is tombstoned.",
    };
  }

  const content = documentContent(lookup.document);
  if (!content) {
    return {
      ...base,
      summary: `Missing registration document ${registrationDocumentId(handle)}.`,
    };
  }

  const registeredHandle = String(content.handle ?? "").trim();
  if (registeredHandle && registeredHandle !== handle) {
    return {
      ...base,
      summary: `Registration doc handle does not match @${handle}.`,
    };
  }

  const registeredActorId = String(content.actor_id ?? "").trim();
  if (!registeredActorId || registeredActorId !== actorId) {
    return {
      ...base,
      summary: "Registration actor does not match the principal actor.",
    };
  }

  const status = String(content.status ?? "active").trim() || "active";
  if (status !== "active") {
    if (status === "pending") {
      return {
        ...base,
        summary:
          "Bridge has not checked in yet. Start the bridge before humans tag this agent.",
      };
    }
    return {
      ...base,
      summary: `Registration status is ${status}.`,
    };
  }

  if (!bindingTarget) {
    return {
      ...base,
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary:
        "Workspace binding status is unavailable because this workspace has no durable workspace ID.",
    };
  }

  const bindings = Array.isArray(content.workspace_bindings)
    ? content.workspace_bindings
    : [];
  const matchingBinding = bindings.find(
    (binding) => String(binding?.workspace_id ?? "").trim() === bindingTarget,
  );

  if (!matchingBinding) {
    return {
      ...base,
      summary: "Registration is not enabled for this workspace.",
    };
  }

  if (matchingBinding.enabled === false) {
    return {
      ...base,
      summary: "Registration is disabled for this workspace.",
    };
  }

  const bridgeProofKey = String(
    content.bridge_signing_public_key_spki_b64 ?? "",
  ).trim();
  if (!bridgeProofKey) {
    return {
      ...base,
      summary: "Registration is missing its bridge proof key.",
    };
  }

  const checkinEventId = String(content.bridge_checkin_event_id ?? "").trim();
  if (!checkinEventId) {
    return {
      ...base,
      summary:
        "Bridge has not checked in yet. Start the bridge before humans tag this agent.",
    };
  }

  const checkinLookup = registrationLookup(bridgeCheckinDoc);
  if (checkinLookup.state === "error") {
    return {
      ...base,
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: "Bridge check-in status is unavailable right now.",
    };
  }
  if (checkinLookup.state === "missing") {
    return {
      ...base,
      summary: `Missing bridge check-in event ${checkinEventId}.`,
    };
  }

  const checkin = eventRecord(checkinLookup.document);
  if (String(checkin?.type ?? "").trim() !== "agent_bridge_checked_in") {
    return {
      ...base,
      summary: `Bridge check-in event ${checkinEventId} is invalid.`,
    };
  }

  const checkinContent = eventPayloadContent(checkinLookup.document);
  if (!checkinContent) {
    return {
      ...base,
      summary: `Bridge check-in event ${checkinEventId} is invalid.`,
    };
  }

  const checkinHandle = String(checkinContent.handle ?? "").trim();
  if (checkinHandle && checkinHandle !== handle) {
    return {
      ...base,
      summary: `Bridge check-in handle does not match @${handle}.`,
    };
  }

  const checkinActorId = String(checkinContent.actor_id ?? "").trim();
  if (!checkinActorId || checkinActorId !== actorId) {
    return {
      ...base,
      summary: "Bridge check-in actor does not match the principal actor.",
    };
  }

  const checkinWorkspaceId = String(checkinContent.workspace_id ?? "").trim();
  if (checkinWorkspaceId !== bindingTarget) {
    return {
      ...base,
      summary: "Bridge check-in is for a different workspace.",
    };
  }

  const bridgeInstanceId = String(
    checkinContent.bridge_instance_id ?? "",
  ).trim();
  if (!bridgeInstanceId) {
    return {
      ...base,
      summary:
        "Bridge instance identity is missing. Let the live bridge rewrite this registration.",
    };
  }
  if (!(await verifyBridgeProof(bridgeProofKey, checkinContent))) {
    return {
      ...base,
      summary: "Bridge readiness proof is invalid.",
    };
  }

  const checkedInAt = parseTimestamp(checkinContent.checked_in_at);
  const expiresAt = parseTimestamp(checkinContent.expires_at);
  if (!checkedInAt) {
    return {
      ...base,
      summary:
        "Bridge has not checked in yet. Start the bridge before humans tag this agent.",
    };
  }
  if (!expiresAt) {
    return {
      ...base,
      badgeLabel: "Unknown",
      badgeClass: "bg-slate-500/10 text-slate-300",
      summary: "Bridge check-in metadata is incomplete right now.",
    };
  }
  if (expiresAt < Date.now()) {
    return {
      ...base,
      summary: "Bridge check-in is stale. Restart or reconnect the bridge.",
    };
  }

  return {
    applicable: true,
    handle,
    wakeable: true,
    badgeLabel: "Wakeable",
    badgeClass: "bg-emerald-500/10 text-emerald-400",
    summary: `Wakeable as @${handle}.`,
  };
}
