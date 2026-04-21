import { describe, expect, it } from "vitest";

import { resolveAuthCapabilities } from "../../src/lib/server/authCapabilities.js";
import { capabilities } from "../fixtures/workspaceAuth.js";

describe("resolveAuthCapabilities", () => {
  it("returns local when ANX_CONTROL_BASE_URL is unset", () => {
    const caps = resolveAuthCapabilities({});
    expect(caps.mode).toBe(capabilities.local.mode);
    expect(caps.supportsCpWorkspaceIdLookup).toBe(false);
  });

  it("returns packed-host-dev with lookup when packed flag and CP URL are set", () => {
    const caps = resolveAuthCapabilities({
      ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
      ANX_SAAS_PACKED_HOST_DEV: "1",
    });
    expect(caps.mode).toBe(capabilities.packedHostDev.mode);
    expect(caps.supportsCpWorkspaceIdLookup).toBe(true);
  });

  it("returns hosted when CP URL is set without packed-host flag", () => {
    const caps = resolveAuthCapabilities({
      ANX_CONTROL_BASE_URL: "https://cp.example",
      ANX_SAAS_PACKED_HOST_DEV: "0",
    });
    expect(caps.mode).toBe(capabilities.hosted.mode);
    expect(caps.supportsCpWorkspaceIdLookup).toBe(false);
  });
});
