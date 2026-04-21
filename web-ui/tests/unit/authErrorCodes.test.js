import { describe, expect, it } from "vitest";

import {
  allAuthErrorCodes,
  AuthErrorCode,
  isKnownAuthErrorCode,
} from "../../src/lib/authErrorCodes.js";

describe("AuthErrorCode", () => {
  it("has unique string values", () => {
    const values = Object.values(AuthErrorCode);
    expect(new Set(values).size).toBe(values.length);
  });

  it("isKnownAuthErrorCode recognizes every exported code", () => {
    for (const code of allAuthErrorCodes()) {
      expect(isKnownAuthErrorCode(code)).toBe(true);
    }
  });
});
