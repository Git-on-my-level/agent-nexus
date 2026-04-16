function decodeBase64Url(value) {
  const normalized = String(value ?? "")
    .replace(/-/g, "+")
    .replace(/_/g, "/");
  const padded = normalized.padEnd(
    normalized.length + ((4 - (normalized.length % 4)) % 4),
    "=",
  );
  const decoded = atob(padded);
  return Uint8Array.from(decoded, (char) => char.charCodeAt(0));
}

function encodeBase64Url(value) {
  const bytes =
    value instanceof Uint8Array
      ? value
      : value instanceof ArrayBuffer
        ? new Uint8Array(value)
        : ArrayBuffer.isView(value)
          ? new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
          : new Uint8Array();

  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }

  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

function mapCredentialDescriptor(descriptor) {
  return {
    ...descriptor,
    id: decodeBase64Url(descriptor.id),
  };
}

/**
 * Auth endpoints return the inner PublicKeyCredential* options as
 * `public_key_options`. The Credential API expects `{ publicKey: { ... } }`.
 * Also accept that shape when callers already wrap.
 */
function normalizeCredentialCreationOptions(options) {
  if (!options || typeof options !== "object") {
    return { publicKey: {} };
  }
  if (
    options.publicKey &&
    typeof options.publicKey === "object" &&
    !Array.isArray(options.publicKey)
  ) {
    return options;
  }
  return { publicKey: options };
}

function normalizeCredentialRequestOptions(options) {
  if (!options || typeof options !== "object") {
    return { publicKey: {} };
  }
  if (
    options.publicKey &&
    typeof options.publicKey === "object" &&
    !Array.isArray(options.publicKey)
  ) {
    return options;
  }
  return { publicKey: options };
}

function mapCreationOptions(options) {
  const normalized = normalizeCredentialCreationOptions(options);
  const pk = normalized.publicKey;
  const user = pk.user;
  return {
    ...normalized,
    publicKey: {
      ...pk,
      challenge: decodeBase64Url(pk.challenge),
      user: user
        ? {
            ...user,
            id: decodeBase64Url(user.id),
          }
        : pk.user,
      excludeCredentials: (pk.excludeCredentials ?? []).map(
        mapCredentialDescriptor,
      ),
    },
  };
}

function mapRequestOptions(options) {
  const normalized = normalizeCredentialRequestOptions(options);
  const pk = normalized.publicKey;
  return {
    ...normalized,
    publicKey: {
      ...pk,
      challenge: decodeBase64Url(pk.challenge),
      allowCredentials: (pk.allowCredentials ?? []).map(
        mapCredentialDescriptor,
      ),
    },
  };
}

function serializeValue(value) {
  if (value instanceof ArrayBuffer || ArrayBuffer.isView(value)) {
    return encodeBase64Url(value);
  }

  if (Array.isArray(value)) {
    return value.map((item) => serializeValue(item));
  }

  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, serializeValue(item)]),
    );
  }

  return value;
}

function readCredentialProperty(value, key) {
  if (!value || typeof value !== "object") {
    return undefined;
  }

  return value[key];
}

function serializeAuthenticatorResponse(response) {
  const serialized = {};

  for (const key of [
    "clientDataJSON",
    "attestationObject",
    "authenticatorData",
    "signature",
    "userHandle",
    "publicKey",
  ]) {
    const value = readCredentialProperty(response, key);
    if (value !== undefined && value !== null) {
      serialized[key] = serializeValue(value);
    }
  }

  const publicKeyAlgorithm = readCredentialProperty(
    response,
    "publicKeyAlgorithm",
  );
  if (publicKeyAlgorithm !== undefined && publicKeyAlgorithm !== null) {
    serialized.publicKeyAlgorithm = publicKeyAlgorithm;
  }

  if (typeof response?.getTransports === "function") {
    serialized.transports = response.getTransports();
  } else {
    const transports = readCredentialProperty(response, "transports");
    if (transports !== undefined && transports !== null) {
      serialized.transports = serializeValue(transports);
    }
  }

  return serialized;
}

function serializePublicKeyCredential(credential) {
  const serialized = {
    id: String(readCredentialProperty(credential, "id") ?? ""),
    rawId: serializeValue(readCredentialProperty(credential, "rawId")),
    response: serializeAuthenticatorResponse(
      readCredentialProperty(credential, "response"),
    ),
    type: String(readCredentialProperty(credential, "type") ?? "public-key"),
  };

  const authenticatorAttachment = readCredentialProperty(
    credential,
    "authenticatorAttachment",
  );
  if (authenticatorAttachment) {
    serialized.authenticatorAttachment = authenticatorAttachment;
  }

  if (typeof credential?.getClientExtensionResults === "function") {
    serialized.clientExtensionResults = serializeValue(
      credential.getClientExtensionResults(),
    );
  } else {
    const extensionResults = readCredentialProperty(
      credential,
      "clientExtensionResults",
    );
    if (extensionResults && typeof extensionResults === "object") {
      serialized.clientExtensionResults = serializeValue(extensionResults);
    }
  }

  return serialized;
}

function assertPasskeySupport() {
  if (
    typeof window === "undefined" ||
    typeof navigator === "undefined" ||
    typeof window.PublicKeyCredential === "undefined"
  ) {
    throw new Error("This browser does not support passkeys.");
  }
}

export async function createPasskeyCredential(options) {
  assertPasskeySupport();
  const credential = await navigator.credentials.create(
    mapCreationOptions(options),
  );
  if (!credential) {
    throw new Error("Passkey registration was cancelled.");
  }
  return serializePublicKeyCredential(credential);
}

export async function getPasskeyAssertion(options) {
  assertPasskeySupport();
  const credential = await navigator.credentials.get(
    mapRequestOptions(options),
  );
  if (!credential) {
    throw new Error("Passkey sign-in was cancelled.");
  }
  return serializePublicKeyCredential(credential);
}
