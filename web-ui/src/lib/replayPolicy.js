import { randomUUID, sha256Hex } from "./cryptoFallback.js";
import policy from "../../../core/internal/replaypolicy/policy.json";

export const replayPolicy = Object.freeze(policy);
export const autoRetryMaxAgeMs =
  policy.autoRetryMaxAgeDays * 24 * 60 * 60 * 1000;

export { randomUUID, sha256Hex };

export function replayExpired(createdAt, now = Date.now()) {
  return (
    !Number.isFinite(createdAt) ||
    createdAt <= 0 ||
    now - createdAt >= autoRetryMaxAgeMs
  );
}
export function expiredReplayError() {
  return Object.assign(
    new Error("The original receipt may no longer be available."),
    {
      replayExpired: true,
      publicMessage:
        "This request is too old for safe retry. It may already have been sent. Copy or edit it, or retry as a new request.",
    },
  );
}

// Strip only transport key locations, never keys inside authored JSON content.
export function requestIntent(request) {
  const value = JSON.parse(JSON.stringify(request));
  delete value.autoDispatchKey;
  delete value.autoReconcileKey;
  for (const body of [
    value,
    value.body,
    value.answer,
    value.autoDispatch,
    value.autoReconcile,
    value.creation,
    value.message,
    ...(value.items || []).map((item) => item.body),
  ]) {
    if (!body) continue;
    delete body.request_key;
    delete body.replay_key;
  }
  return value;
}

export async function intentFingerprint(request) {
  const canonical = (value) =>
    Array.isArray(value)
      ? value.map(canonical)
      : value && typeof value === "object"
        ? Object.fromEntries(
            Object.keys(value)
              .sort()
              .map((key) => [key, canonical(value[key])]),
          )
        : value;
  const bytes = new TextEncoder().encode(
    JSON.stringify(canonical(requestIntent(request))),
  );
  return sha256Hex(bytes);
}

// Legacy saved items predate persisted stage seeds. Derive their fallback
// from the durable outbox identity, so independent recovery copies agree.
export async function autoApplyStageKey(entry, stage) {
  if (!["autoDispatch", "autoReconcile"].includes(stage))
    throw new Error("Unknown auto-apply stage.");
  const saved =
    entry.request[stage]?.request_key || entry.request[`${stage}Key`];
  if (saved) return saved;
  if (!entry.key) throw new Error("Saved mutation identity is missing.");
  return sha256Hex(
    new TextEncoder().encode(
      JSON.stringify(["anx:auto-apply:v1", entry.key, stage]),
    ),
  );
}

export function freshRequest(request, operation = "", receipt = null) {
  const value = JSON.parse(JSON.stringify(request));
  for (const body of [
    value,
    value.body,
    value.answer,
    value.autoDispatch,
    value.autoReconcile,
    value.creation,
    value.message,
    ...(value.items || []).map((item) => item.body),
  ]) {
    if (!body) continue;
    // A partial batch's confirmed members remain attached to their old keys.
    // Explicit renewal rotates only requests whose outcome is still unknown.
    if (
      operation === "inbox.controls" &&
      receipt?.controls?.some((done) => done.key === body.request_key)
    )
      continue;
    if (
      operation === "pm.decision.answer" &&
      ((body === value.answer && receipt?.decision) ||
        (body === value.autoDispatch && receipt?.dispatchedAction) ||
        (body === value.autoReconcile && receipt?.reconciledAction))
    )
      continue;
    for (const field of ["request_key", "replay_key"]) {
      if (field in body) body[field] = randomUUID();
    }
  }
  if (operation === "pm.decision.answer") {
    if ("autoDispatchKey" in value && !receipt?.dispatchedAction) {
      value.autoDispatchKey = randomUUID();
    }
    if ("autoReconcileKey" in value && !receipt?.reconciledAction) {
      value.autoReconcileKey = randomUUID();
    }
  }
  // This is an explicit new intent, not recovery of the old body. Legacy PM
  // requests now opt into receipts just like any newly queued PM request.
  if (operation === "pm.chat.send") {
    for (const body of [value.creation, value.message]) {
      if (!body) continue;
      body.request_key ||= randomUUID();
      body.replay_key ||= randomUUID();
    }
  }
  return value;
}
