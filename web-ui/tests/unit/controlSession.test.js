import { beforeEach, describe, expect, it, vi } from "vitest";

const controlClientState = {
  finishPasskeyRegistration: vi.fn(),
  finishSession: vi.fn(),
  startPasskeyRegistration: vi.fn(),
  startSession: vi.fn(),
  validateSession: vi.fn(),
};

const createControlClientMock = vi.hoisted(() =>
  vi.fn(() => ({
    finishPasskeyRegistration: controlClientState.finishPasskeyRegistration,
    finishSession: controlClientState.finishSession,
    startPasskeyRegistration: controlClientState.startPasskeyRegistration,
    startSession: controlClientState.startSession,
    validateSession: controlClientState.validateSession,
  })),
);

vi.mock("../../src/lib/server/controlClient.js", () => ({
  createControlClient: createControlClientMock,
}));

import {
  clearControlSessionState,
  finishControlLogin,
  finishControlRegistration,
  loadControlSession,
  startControlLogin,
  startControlRegistration,
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

  it("forwards the browser origin to control-plane WebAuthn start calls", async () => {
    controlClientState.startPasskeyRegistration.mockResolvedValue({
      registration_session_id: "registration-1",
    });
    controlClientState.startSession.mockResolvedValue({
      session_id: "session-1",
    });
    const event = {
      url: new URL("https://oar.example.test/auth"),
      request: {
        headers: new Headers({
          origin: "https://app.example.test",
        }),
      },
      cookies: createEvent().cookies,
    };

    await startControlRegistration(event, "ops@example.com", "Ops");
    await startControlLogin(event, "ops@example.com");

    expect(createControlClientMock).toHaveBeenNthCalledWith(1, undefined, {
      origin: "https://app.example.test",
    });
    expect(createControlClientMock).toHaveBeenNthCalledWith(2, undefined, {
      origin: "https://app.example.test",
    });
  });

  it("falls back to the UI origin for WebAuthn finish calls when the browser omits Origin", async () => {
    controlClientState.finishPasskeyRegistration.mockResolvedValue({
      account: null,
      session: null,
    });
    controlClientState.finishSession.mockResolvedValue({
      account: null,
      session: null,
    });
    const event = createEvent();

    await finishControlRegistration(
      event,
      "registration-1",
      { id: "credential-1" },
      "",
    );
    await finishControlLogin(event, "session-1", { id: "credential-1" }, "");

    expect(createControlClientMock).toHaveBeenNthCalledWith(1, undefined, {
      origin: "https://oar.example.test",
    });
    expect(createControlClientMock).toHaveBeenNthCalledWith(2, undefined, {
      origin: "https://oar.example.test",
    });
  });
});
