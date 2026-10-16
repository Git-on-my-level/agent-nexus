import policy from "../../../core/internal/replaypolicy/policy.json";

export const replayPolicy = Object.freeze(policy);
export const autoRetryMaxAgeMs =
  policy.autoRetryMaxAgeDays * 24 * 60 * 60 * 1000;

const SHA256_INIT = new Uint32Array([
  0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c,
  0x1f83d9ab, 0x5be0cd19,
]);

const SHA256_K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1,
  0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
  0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
  0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
  0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
  0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

function rotr(value, shift) {
  return (value >>> shift) | (value << (32 - shift));
}

function sha256PureJsHex(inputBytes) {
  const bytes =
    inputBytes instanceof Uint8Array
      ? inputBytes
      : new Uint8Array(inputBytes || []);
  const len = bytes.length;
  const bitLenHi = Math.floor(len / 0x20000000);
  const bitLenLo = (len << 3) >>> 0;
  const paddedLen = ((len + 9 + 63) >> 6) << 6;
  const padded = new Uint8Array(paddedLen);
  padded.set(bytes);
  padded[len] = 0x80;
  const view = new DataView(padded.buffer);
  view.setUint32(paddedLen - 8, bitLenHi, false);
  view.setUint32(paddedLen - 4, bitLenLo, false);

  const state = new Uint32Array(SHA256_INIT);
  const w = new Uint32Array(64);
  for (let offset = 0; offset < paddedLen; offset += 64) {
    for (let i = 0; i < 16; i += 1) {
      w[i] = view.getUint32(offset + i * 4, false);
    }
    for (let i = 16; i < 64; i += 1) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0;
    }
    let [a, b, c, d, e, f, g, h] = state;
    for (let i = 0; i < 64; i += 1) {
      const s1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      const ch = (e & f) ^ (~e & g);
      const temp1 = (h + s1 + ch + SHA256_K[i] + w[i]) | 0;
      const s0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const temp2 = (s0 + maj) | 0;
      h = g;
      g = f;
      f = e;
      e = (d + temp1) | 0;
      d = c;
      c = b;
      b = a;
      a = (temp1 + temp2) | 0;
    }
    state[0] = (state[0] + a) | 0;
    state[1] = (state[1] + b) | 0;
    state[2] = (state[2] + c) | 0;
    state[3] = (state[3] + d) | 0;
    state[4] = (state[4] + e) | 0;
    state[5] = (state[5] + f) | 0;
    state[6] = (state[6] + g) | 0;
    state[7] = (state[7] + h) | 0;
  }
  return Array.from(state, (word) =>
    (word >>> 0).toString(16).padStart(8, "0"),
  ).join("");
}

/**
 * Generate an RFC 4122 v4 UUID using crypto.randomUUID when available, or
 * crypto.getRandomValues on non-secure (plain-HTTP) contexts.
 */
export function randomUUID() {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }
  const bytes = new Uint8Array(16);
  if (typeof globalThis.crypto?.getRandomValues === "function") {
    globalThis.crypto.getRandomValues(bytes);
  } else {
    for (let i = 0; i < 16; i += 1) {
      bytes[i] = Math.floor(Math.random() * 256);
    }
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0"));
  return `${hex.slice(0, 4).join("")}-${hex.slice(4, 6).join("")}-${hex.slice(6, 8).join("")}-${hex.slice(8, 10).join("")}-${hex.slice(10, 16).join("")}`;
}

export async function sha256Hex(bytes, { forceFallback = false } = {}) {
  if (
    !forceFallback &&
    typeof globalThis.crypto?.subtle?.digest === "function"
  ) {
    const hash = await globalThis.crypto.subtle.digest("SHA-256", bytes);
    return Array.from(new Uint8Array(hash), (byte) =>
      byte.toString(16).padStart(2, "0"),
    ).join("");
  }
  return sha256PureJsHex(bytes);
}

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
