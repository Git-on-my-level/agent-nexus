import { describe, expect, it } from "vitest";

import { coreBaseUrlForNodeFetch } from "../../src/lib/server/coreBaseUrlForNodeFetch.js";
import { coreEndpointURL } from "../../src/lib/server/coreEndpoint.js";

describe("coreBaseUrlForNodeFetch", () => {
  it("returns empty for empty / whitespace", () => {
    expect(coreBaseUrlForNodeFetch("")).toBe("");
    expect(coreBaseUrlForNodeFetch("  ")).toBe("");
  });

  it("maps localhost to 127.0.0.1 preserving port and path prefix", () => {
    expect(coreBaseUrlForNodeFetch("http://localhost:9980/ws/a/b")).toBe(
      "http://127.0.0.1:9980/ws/a/b",
    );
  });

  it("is idempotent for 127.0.0.1", () => {
    const once = coreBaseUrlForNodeFetch("http://127.0.0.1:9/foo");
    expect(coreBaseUrlForNodeFetch(once)).toBe(once);
  });

  it("trims trailing slashes for parseable URLs", () => {
    expect(coreBaseUrlForNodeFetch("http://127.0.0.1:1/api/")).toBe(
      "http://127.0.0.1:1/api",
    );
  });

  it("trims trailing slashes for invalid URL strings (catch path)", () => {
    expect(coreBaseUrlForNodeFetch("not-a-url///")).toBe("not-a-url");
  });
});

describe("coreEndpointURL join (with normalized base from coreBaseUrlForNodeFetch)", () => {
  it("joins with and without trailing slash on base; pathname has no query", () => {
    const baseA = coreBaseUrlForNodeFetch("http://localhost:1/prefix/");
    const baseB = coreBaseUrlForNodeFetch("http://localhost:1/prefix");
    const path = "/auth/token";
    expect(coreEndpointURL(baseA, path)).toBe(
      "http://127.0.0.1:1/prefix/auth/token",
    );
    expect(coreEndpointURL(baseB, path)).toBe(
      "http://127.0.0.1:1/prefix/auth/token",
    );
    expect(coreEndpointURL(baseA, path)).not.toMatch(/[?#]/);
  });
});
