import { afterEach, describe, expect, it, vi } from "vitest";

import {
  createPasskeyCredential,
  getPasskeyAssertion,
} from "../../src/lib/passkeyBrowser.js";

function byteArray(...values) {
  return Uint8Array.from(values);
}

function setBrowserSupport({ createResult, getResult } = {}) {
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: {
      PublicKeyCredential: class PublicKeyCredential {},
    },
  });

  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: {
      credentials: {
        create: vi.fn().mockResolvedValue(createResult),
        get: vi.fn().mockResolvedValue(getResult),
      },
    },
  });
}

function defineHiddenProperty(target, key, value) {
  Object.defineProperty(target, key, {
    configurable: true,
    enumerable: false,
    value,
  });
}

function makeRegistrationCredential() {
  const response = {};
  defineHiddenProperty(
    response,
    "attestationObject",
    byteArray(1, 2, 3).buffer,
  );
  defineHiddenProperty(response, "clientDataJSON", byteArray(4, 5, 6).buffer);
  response.getTransports = () => ["internal"];

  const credential = {};
  defineHiddenProperty(credential, "id", "credential-id");
  defineHiddenProperty(credential, "rawId", byteArray(9, 8).buffer);
  defineHiddenProperty(credential, "response", response);
  defineHiddenProperty(credential, "type", "public-key");
  defineHiddenProperty(credential, "authenticatorAttachment", "platform");
  credential.getClientExtensionResults = () => ({ credProps: { rk: true } });
  return credential;
}

function makeAssertionCredential() {
  const response = {};
  defineHiddenProperty(response, "authenticatorData", byteArray(10, 11).buffer);
  defineHiddenProperty(response, "clientDataJSON", byteArray(12, 13).buffer);
  defineHiddenProperty(response, "signature", byteArray(14, 15, 16).buffer);
  defineHiddenProperty(response, "userHandle", byteArray(17, 18).buffer);

  const credential = {};
  defineHiddenProperty(credential, "id", "assertion-id");
  defineHiddenProperty(credential, "rawId", byteArray(19, 20).buffer);
  defineHiddenProperty(credential, "response", response);
  defineHiddenProperty(credential, "type", "public-key");
  return credential;
}

afterEach(() => {
  vi.restoreAllMocks();
  delete globalThis.window;
  delete globalThis.navigator;
});

describe("passkeyBrowser", () => {
  it("serializes non-enumerable registration credential fields", async () => {
    const credential = makeRegistrationCredential();
    setBrowserSupport({ createResult: credential });

    const result = await createPasskeyCredential({
      publicKey: {
        challenge: "AQID",
        user: {
          id: "BAUG",
        },
      },
    });

    expect(result).toEqual({
      authenticatorAttachment: "platform",
      clientExtensionResults: { credProps: { rk: true } },
      id: "credential-id",
      rawId: "CQg",
      response: {
        attestationObject: "AQID",
        clientDataJSON: "BAUG",
        transports: ["internal"],
      },
      type: "public-key",
    });
  });

  it("serializes non-enumerable assertion credential fields", async () => {
    const credential = makeAssertionCredential();
    setBrowserSupport({ getResult: credential });

    const result = await getPasskeyAssertion({
      publicKey: {
        challenge: "AQID",
      },
    });

    expect(result).toEqual({
      id: "assertion-id",
      rawId: "ExQ",
      response: {
        authenticatorData: "Cgs",
        clientDataJSON: "DA0",
        signature: "Dg8Q",
        userHandle: "ERI",
      },
      type: "public-key",
    });
  });
});
