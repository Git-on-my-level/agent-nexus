import { describe, expect, it } from "vitest";

import { isProxyableCommand } from "../../src/lib/coreRouteCatalog.js";
import { isDirectCoreProxyPath } from "../../src/lib/server/directCoreProxyPaths.js";

describe("directCoreProxyParity", () => {
  it("covers GET /events/stream via the generated catalog (not hook-only)", () => {
    expect(isProxyableCommand("GET", "/events/stream")).toBe(true);
    expect(isDirectCoreProxyPath("GET", "/events/stream")).toBe(false);
  });

  it("covers GET /meta/handshake via OpenAPI (not hook-only)", () => {
    expect(isProxyableCommand("GET", "/meta/handshake")).toBe(true);
    expect(isDirectCoreProxyPath("GET", "/meta/handshake")).toBe(false);
  });

  it("covers /actors via OpenAPI", () => {
    expect(isProxyableCommand("GET", "/actors")).toBe(true);
    expect(isProxyableCommand("POST", "/actors")).toBe(true);
    expect(isDirectCoreProxyPath("GET", "/actors")).toBe(false);
    expect(isDirectCoreProxyPath("POST", "/actors")).toBe(false);
  });

  it("lists core auth APIs in OpenAPI; web-ui-owned paths stay out of the catalog", () => {
    expect(isProxyableCommand("GET", "/auth/session")).toBe(false);
    expect(isProxyableCommand("POST", "/auth/token")).toBe(true);
    expect(isProxyableCommand("GET", "/auth/bootstrap/status")).toBe(true);
    expect(isDirectCoreProxyPath("GET", "/auth/session")).toBe(false);
    expect(isDirectCoreProxyPath("GET", "/auth/dev/identities")).toBe(false);
    expect(isDirectCoreProxyPath("POST", "/auth/dev/session")).toBe(false);
    expect(isDirectCoreProxyPath("POST", "/auth/token")).toBe(false);
    expect(isDirectCoreProxyPath("GET", "/auth/bootstrap/status")).toBe(false);
  });
});
