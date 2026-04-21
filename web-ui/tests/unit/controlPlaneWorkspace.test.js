import { describe, expect, it, vi } from "vitest";

import {
  fetchWorkspaceByIdFromControlPlane,
  isHostedWebUiShell,
  isSaasPackedHostDev,
} from "../../src/lib/server/controlPlaneWorkspace.js";
import { controlPlaneWorkspaceRows } from "../fixtures/workspaceAuth.js";

describe("controlPlaneWorkspace", () => {
  it("isSaasPackedHostDev is false when unset", () => {
    expect(isSaasPackedHostDev({})).toBe(false);
  });

  it("isHostedWebUiShell is false without packed host flag", () => {
    expect(
      isHostedWebUiShell({ ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100" }),
    ).toBe(false);
  });

  it("isHostedWebUiShell is false without control base URL", () => {
    expect(isHostedWebUiShell({ ANX_SAAS_PACKED_HOST_DEV: "1" })).toBe(false);
  });

  it("isHostedWebUiShell is true when packed host and control base URL are set", () => {
    expect(
      isHostedWebUiShell({
        ANX_SAAS_PACKED_HOST_DEV: "1",
        ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
      }),
    ).toBe(true);
  });
});

describe("fetchWorkspaceByIdFromControlPlane", () => {
  it("returns null when packed-host dev flag is off", async () => {
    const out = await fetchWorkspaceByIdFromControlPlane({
      env: {
        ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
        ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "tok",
      },
      workspaceId: "ws-1",
      fetchFn: vi.fn(),
      getCookie: () => "",
    });
    expect(out).toBeNull();
  });

  it("maps 200 workspace payload", async () => {
    const fetchFn = vi.fn(async () =>
      new Response(JSON.stringify({ workspace: controlPlaneWorkspaceRows.minimal }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
    const out = await fetchWorkspaceByIdFromControlPlane({
      env: {
        ANX_SAAS_PACKED_HOST_DEV: "1",
        ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
        ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "env-tok",
      },
      workspaceId: "ws-cp-1",
      fetchFn,
      getCookie: () => "",
    });
    expect(out?.slug).toBe("alpha");
    expect(fetchFn).toHaveBeenCalledWith(
      expect.stringContaining("/workspaces/ws-cp-1"),
      expect.objectContaining({
        headers: expect.objectContaining({
          authorization: "Bearer env-tok",
        }),
      }),
    );
  });

  it("returns null on 401 and 404", async () => {
    for (const status of [401, 404]) {
      const fetchFn = vi.fn(async () => new Response("", { status }));
      const out = await fetchWorkspaceByIdFromControlPlane({
        env: {
          ANX_SAAS_PACKED_HOST_DEV: "1",
          ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
          ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "tok",
        },
        workspaceId: "x",
        fetchFn,
        getCookie: () => "",
      });
      expect(out).toBeNull();
    }
  });

  it("uses cookie token when env token is unset", async () => {
    const fetchFn = vi.fn(async () =>
      new Response(JSON.stringify({ workspace: controlPlaneWorkspaceRows.minimal }), {
        status: 200,
      }),
    );
    await fetchWorkspaceByIdFromControlPlane({
      env: {
        ANX_SAAS_PACKED_HOST_DEV: "1",
        ANX_CONTROL_BASE_URL: "http://127.0.0.1:8100",
      },
      workspaceId: "ws-cp-1",
      fetchFn,
      getCookie: (name) =>
        name === "oar_cp_dev_access_token" ? "cookie-tok" : "",
    });
    expect(fetchFn.mock.calls[0][1].headers.authorization).toBe(
      "Bearer cookie-tok",
    );
  });
});
