import { describe, expect, it } from "vitest";

import {
  isHostedWebUiShell,
  isSaasPackedHostDev,
} from "../../src/lib/server/controlPlaneWorkspace.js";

describe("controlPlaneWorkspace", () => {
  it("isSaasPackedHostDev is false when unset", () => {
    expect(isSaasPackedHostDev({})).toBe(false);
  });

  it("isHostedWebUiShell is false without packed host flag", () => {
    expect(
      isHostedWebUiShell({ OAR_CONTROL_BASE_URL: "http://127.0.0.1:8100" }),
    ).toBe(false);
  });

  it("isHostedWebUiShell is false without control base URL", () => {
    expect(isHostedWebUiShell({ OAR_SAAS_PACKED_HOST_DEV: "1" })).toBe(false);
  });

  it("isHostedWebUiShell is true when packed host and control base URL are set", () => {
    expect(
      isHostedWebUiShell({
        OAR_SAAS_PACKED_HOST_DEV: "1",
        OAR_CONTROL_BASE_URL: "http://127.0.0.1:8100",
      }),
    ).toBe(true);
  });
});
