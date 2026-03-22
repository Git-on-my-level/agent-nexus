import { describe, expect, it, vi, beforeEach } from "vitest";

const controlSessionMocks = vi.hoisted(() => ({
  clearControlInviteToken: vi.fn(),
  finishControlLogin: vi.fn(),
  finishControlRegistration: vi.fn(),
  loadControlSession: vi.fn(),
  logoutControlSession: vi.fn(),
  readControlInviteToken: vi.fn(() => ""),
  startControlLogin: vi.fn(),
  startControlRegistration: vi.fn(),
}));

vi.mock("../../src/lib/server/controlSession.js", () => ({
  clearControlInviteToken: controlSessionMocks.clearControlInviteToken,
  finishControlLogin: controlSessionMocks.finishControlLogin,
  finishControlRegistration: controlSessionMocks.finishControlRegistration,
  loadControlSession: controlSessionMocks.loadControlSession,
  logoutControlSession: controlSessionMocks.logoutControlSession,
  readControlInviteToken: controlSessionMocks.readControlInviteToken,
  startControlLogin: controlSessionMocks.startControlLogin,
  startControlRegistration: controlSessionMocks.startControlRegistration,
}));

import { POST } from "../../src/routes/auth/+server.js";

function createEvent(body) {
  return {
    request: {
      async json() {
        return body;
      },
    },
    url: new URL("https://oar.example.com/auth"),
    cookies: {
      get() {
        return null;
      },
      set() {},
      delete() {},
    },
  };
}

describe("auth route", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    controlSessionMocks.readControlInviteToken.mockReturnValue("");
  });

  it("returns the full account/session envelope after passkey registration finish", async () => {
    controlSessionMocks.finishControlRegistration.mockResolvedValue({
      account: {
        id: "account-1",
        email: "ops@example.com",
      },
      session: {
        access_token: "access-token-1",
      },
    });

    const response = await POST(
      createEvent({
        action: "register-finish",
        registration_session_id: "registration-1",
        credential: { id: "credential-1" },
      }),
    );

    expect(await response.json()).toEqual({
      account: {
        id: "account-1",
        email: "ops@example.com",
      },
      session: {
        access_token: "access-token-1",
      },
    });
    expect(controlSessionMocks.finishControlRegistration).toHaveBeenCalledWith(
      expect.anything(),
      "registration-1",
      { id: "credential-1" },
      "",
    );
  });

  it("preserves upstream control status and error envelope on registration start failure", async () => {
    controlSessionMocks.startControlRegistration.mockRejectedValue(
      Object.assign(new Error("Account already exists."), {
        status: 409,
        body: {
          error: {
            code: "account_exists",
            message: "Account already exists.",
          },
        },
      }),
    );

    const response = await POST(
      createEvent({
        action: "register-start",
        email: "ops@example.com",
        display_name: "Ops Lead",
      }),
    );

    expect(response.status).toBe(409);
    expect(await response.json()).toEqual({
      error: {
        code: "account_exists",
        message: "Account already exists.",
      },
    });
  });

  it("rejects invalid JSON before calling the auth helpers", async () => {
    const response = await POST({
      request: {
        async json() {
          throw new Error("invalid json");
        },
      },
      url: new URL("https://oar.example.com/auth"),
      cookies: {
        get() {
          return null;
        },
        set() {},
        delete() {},
      },
    });

    expect(response.status).toBe(400);
    expect(await response.json()).toEqual({
      error: {
        code: "invalid_json",
        message: "Request body must be valid JSON.",
      },
    });
    expect(controlSessionMocks.startControlRegistration).not.toHaveBeenCalled();
  });

  it("forwards invite tokens from the invite cookie on registration and login completion", async () => {
    controlSessionMocks.finishControlRegistration.mockResolvedValue({
      account: { id: "account-1", email: "ops@example.com" },
      session: { access_token: "token-1" },
    });
    controlSessionMocks.finishControlLogin.mockResolvedValue({
      account: { id: "account-1", email: "ops@example.com" },
      session: { access_token: "token-2" },
    });
    controlSessionMocks.readControlInviteToken.mockReturnValue(
      "invite-token-1",
    );

    await POST(
      createEvent({
        action: "register-finish",
        registration_session_id: "registration-1",
        credential: { id: "credential-1" },
      }),
    );
    await POST(
      createEvent({
        action: "login-finish",
        session_id: "session-1",
        credential: { id: "credential-2" },
      }),
    );

    expect(controlSessionMocks.finishControlRegistration).toHaveBeenCalledWith(
      expect.anything(),
      "registration-1",
      { id: "credential-1" },
      "invite-token-1",
    );
    expect(controlSessionMocks.finishControlLogin).toHaveBeenCalledWith(
      expect.anything(),
      "session-1",
      { id: "credential-2" },
      "invite-token-1",
    );
    expect(controlSessionMocks.clearControlInviteToken).toHaveBeenCalledTimes(
      2,
    );
  });

  it("clears stale invite cookies after invalid invite token failures", async () => {
    controlSessionMocks.readControlInviteToken.mockReturnValue(
      "stale-invite-token",
    );
    controlSessionMocks.finishControlLogin.mockRejectedValue(
      Object.assign(new Error("Invite token is invalid or expired."), {
        status: 401,
        body: {
          error: {
            code: "invalid_token",
            message: "Invite token is invalid or expired.",
          },
        },
      }),
    );

    const response = await POST(
      createEvent({
        action: "login-finish",
        session_id: "session-1",
        credential: { id: "credential-2" },
      }),
    );

    expect(response.status).toBe(401);
    expect(controlSessionMocks.clearControlInviteToken).toHaveBeenCalledTimes(
      1,
    );
  });
});
