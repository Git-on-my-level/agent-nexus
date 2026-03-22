import { beforeEach, describe, expect, it, vi } from "vitest";

const controlClientState = {
  validateSession: vi.fn(),
};

vi.mock("../../src/lib/server/controlClient.js", () => ({
  createControlClient: vi.fn(() => ({
    validateSession: controlClientState.validateSession,
  })),
}));

import {
  loadControlSession,
  clearControlSessionState,
} from "../../src/lib/server/controlSession.js";

function createEvent({ accessToken, account } = {}) {
  const cookies = new Map();
  if (accessToken) {
    cookies.set("oar_control_session", accessToken);
  }
  if (account) {
    cookies.set("oar_control_account", JSON.stringify(account));
  }

  return {
    url: new URL("https://oar.example.test/dashboard"),
    cookies: {
      get(name) {
        return cookies.get(name) ?? null;
      },
      set(name, value) {
        cookies.set(name, value);
      },
      delete(name) {
        cookies.delete(name);
      },
    },
  };
}

describe("control session loading", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearControlSessionState();
  });

  it("clears cached control auth when the control plane rejects the token", async () => {
    controlClientState.validateSession.mockRejectedValue(
      Object.assign(new Error("unauthorized"), { status: 401 }),
    );
    const event = createEvent({
      accessToken: "control-token",
      account: { id: "account-1", email: "ops@example.com" },
    });

    const session = await loadControlSession(event);

    expect(session).toBeNull();
    expect(event.cookies.get("oar_control_session")).toBeNull();
    expect(event.cookies.get("oar_control_account")).toBeNull();
  });

  it("preserves the cached session when validation fails transiently", async () => {
    controlClientState.validateSession.mockRejectedValue(new Error("boom"));
    const event = createEvent({
      accessToken: "control-token",
      account: { id: "account-1", email: "ops@example.com" },
    });

    const session = await loadControlSession(event);

    expect(session).toEqual({
      accessToken: "control-token",
      account: { id: "account-1", email: "ops@example.com" },
    });
    expect(event.cookies.get("oar_control_session")).toBe("control-token");
  });
});
