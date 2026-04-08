import { describe, expect, it } from "vitest";

import { isProxyableCommand } from "../../src/lib/coreRouteCatalog.js";
import { isDirectCoreProxyPath } from "../../src/lib/server/directCoreProxyPaths.js";

describe("directCoreProxyParity", () => {
  it("covers GET /events/stream via the generated catalog (not hook-only)", () => {
    expect(isProxyableCommand("GET", "/events/stream")).toBe(true);
    expect(isDirectCoreProxyPath("GET", "/events/stream")).toBe(false);
  });

  it("keeps handshake as hook-only until registered in OpenAPI", () => {
    expect(isProxyableCommand("GET", "/meta/handshake")).toBe(false);
    expect(isDirectCoreProxyPath("GET", "/meta/handshake")).toBe(true);
  });

  it("keeps /actors as hook-only", () => {
    expect(isProxyableCommand("GET", "/actors")).toBe(false);
    expect(isProxyableCommand("POST", "/actors")).toBe(false);
    expect(isDirectCoreProxyPath("GET", "/actors")).toBe(true);
    expect(isDirectCoreProxyPath("POST", "/actors")).toBe(true);
  });

  it("proxies core auth APIs but not web-ui-owned /auth/session or /auth/dev", () => {
    expect(isProxyableCommand("GET", "/auth/session")).toBe(false);
    expect(isDirectCoreProxyPath("GET", "/auth/session")).toBe(false);
    expect(isDirectCoreProxyPath("GET", "/auth/dev/identities")).toBe(false);
    expect(isDirectCoreProxyPath("POST", "/auth/dev/session")).toBe(false);
    expect(isDirectCoreProxyPath("POST", "/auth/token")).toBe(true);
    expect(isDirectCoreProxyPath("GET", "/auth/bootstrap/status")).toBe(true);
  });
});
