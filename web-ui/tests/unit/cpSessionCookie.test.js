import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.resetModules();
});

async function loadModule(dev) {
  vi.doMock("$app/environment", () => ({
    dev,
    browser: false,
  }));
  return import("../../src/lib/server/outOfWorkspace/cpSessionCookie.js");
}

describe("readHostedControlPlaneProxyBearer", () => {
  it("never uses env token", async () => {
    const { readHostedControlPlaneProxyBearer } = await loadModule(false);
    const event = {
      cookies: {
        get: (name) => (name === "anx_cp_access_token" ? "sess" : ""),
      },
    };
    expect(readHostedControlPlaneProxyBearer(event)).toBe("sess");
  });

  it("returns empty when cookies are empty even if env is set in shell", async () => {
    const { readHostedControlPlaneProxyBearer } = await loadModule(false);
    const event = { cookies: { get: () => "" } };
    expect(readHostedControlPlaneProxyBearer(event)).toBe("");
  });
});

describe("readHostedControlPlaneAccessToken", () => {
  it("prefers env token in dev (vite dev) when set", async () => {
    const { readHostedControlPlaneAccessToken } = await loadModule(true);
    const event = {
      cookies: {
        get: (name) => (name === "anx_cp_access_token" ? "sess" : ""),
      },
    };
    expect(
      readHostedControlPlaneAccessToken(event, {
        ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "envtok",
      }),
    ).toBe("envtok");
  });

  it("ignores ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN in production", async () => {
    const { readHostedControlPlaneAccessToken } = await loadModule(false);
    const event = {
      cookies: {
        get: (name) => (name === "anx_cp_access_token" ? "sess" : ""),
      },
    };
    expect(
      readHostedControlPlaneAccessToken(event, {
        ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN: "envtok",
      }),
    ).toBe("sess");
  });

  it("uses HttpOnly session cookie when no env token", async () => {
    const { readHostedControlPlaneAccessToken } = await loadModule(false);
    const event = {
      cookies: {
        get: (name) => (name === "anx_cp_access_token" ? "sess" : ""),
      },
    };
    expect(readHostedControlPlaneAccessToken(event, {})).toBe("sess");
  });

  it("ignores dev cookie when dev is false", async () => {
    const { readHostedControlPlaneAccessToken } = await loadModule(false);
    const event = {
      cookies: {
        get: (name) => (name === "anx_cp_dev_access_token" ? "devtok" : ""),
      },
    };
    expect(readHostedControlPlaneAccessToken(event, {})).toBe("");
  });

  it("falls back to dev cookie when dev is true", async () => {
    const { readHostedControlPlaneAccessToken } = await loadModule(true);
    const event = {
      cookies: {
        get: (name) => (name === "anx_cp_dev_access_token" ? "devtok" : ""),
      },
    };
    expect(readHostedControlPlaneAccessToken(event, {})).toBe("devtok");
  });

  it("prefers session cookie over dev cookie when dev is true", async () => {
    const { readHostedControlPlaneAccessToken } = await loadModule(true);
    const event = {
      cookies: {
        get: (name) => {
          if (name === "anx_cp_access_token") return "sess";
          if (name === "anx_cp_dev_access_token") return "devtok";
          return "";
        },
      },
    };
    expect(readHostedControlPlaneAccessToken(event, {})).toBe("sess");
  });
});
