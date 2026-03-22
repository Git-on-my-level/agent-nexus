import { describe, expect, it, vi, beforeEach } from "vitest";

const controlSessionMocks = vi.hoisted(() => ({
  finishControlLogin: vi.fn(),
  finishControlRegistration: vi.fn(),
  loadControlSession: vi.fn(),
  logoutControlSession: vi.fn(),
  startControlLogin: vi.fn(),
  startControlRegistration: vi.fn(),
}));

vi.mock("../../src/lib/server/controlSession.js", () => ({
  finishControlLogin: controlSessionMocks.finishControlLogin,
  finishControlRegistration: controlSessionMocks.finishControlRegistration,
  loadControlSession: controlSessionMocks.loadControlSession,
  logoutControlSession: controlSessionMocks.logoutControlSession,
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
});
