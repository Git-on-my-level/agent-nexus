import { describe, expect, it } from "vitest";

import {
  clearWorkspaceAccessToken,
  clearWorkspaceRefreshToken,
  handleWorkspaceAuthVerifyResponse,
  writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken,
} from "../../src/lib/server/authSession.js";

function createCookieRecorder() {
  const setCalls = [];
  const deleteCalls = [];
  return {
    setCalls,
    deleteCalls,
    cookies: {
      get() {
        return null;
      },
      set(name, value, options) {
        setCalls.push({ name, value, options });
      },
      delete(name, options) {
        deleteCalls.push({ name, options });
      },
    },
  };
}

describe("server auth session helpers", () => {
  it("writes HttpOnly and Secure refresh-token cookies on HTTPS", () => {
    const recorder = createCookieRecorder();
    const event = {
      url: new URL("https://oar.example.com/auth/session"),
      cookies: recorder.cookies,
    };

    writeWorkspaceRefreshToken(event, "alpha", "refresh-token");

    expect(recorder.setCalls).toHaveLength(1);
    expect(recorder.setCalls[0]).toMatchObject({
      name: "oar_ui_session_alpha",
      value: "refresh-token",
      options: {
        httpOnly: true,
        sameSite: "lax",
        secure: true,
        path: "/",
      },
    });
  });

  it("writes HttpOnly and Secure access-token cookies on HTTPS", () => {
    const recorder = createCookieRecorder();
    const event = {
      url: new URL("https://oar.example.com/auth/session"),
      cookies: recorder.cookies,
    };

    writeWorkspaceAccessToken(event, "alpha", "access-token");

    expect(recorder.setCalls).toHaveLength(1);
    expect(recorder.setCalls[0]).toMatchObject({
      name: "oar_ui_access_alpha",
      value: "access-token",
      options: {
        httpOnly: true,
        sameSite: "lax",
        secure: true,
        path: "/",
      },
    });
  });

  it("clears refresh-token cookies with the same workspace scope", () => {
    const recorder = createCookieRecorder();
    const event = {
      cookies: recorder.cookies,
    };

    clearWorkspaceRefreshToken(event, "alpha");

    expect(recorder.deleteCalls).toEqual([
      {
        name: "oar_ui_session_alpha",
        options: {
          path: "/",
        },
      },
    ]);
  });

  it("clears access-token cookies with the same workspace scope", () => {
    const recorder = createCookieRecorder();
    const event = {
      cookies: recorder.cookies,
    };

    clearWorkspaceAccessToken(event, "alpha");

    expect(recorder.deleteCalls).toEqual([
      {
        name: "oar_ui_access_alpha",
        options: {
          path: "/",
        },
      },
    ]);
  });

  it("sanitizes auth verify responses before returning them to the browser", async () => {
    const recorder = createCookieRecorder();
    const event = {
      url: new URL("https://oar.example.com/auth/passkey/login/verify"),
      cookies: recorder.cookies,
    };
    const upstreamResponse = new Response(
      JSON.stringify({
        agent: {
          agent_id: "agent-1",
          actor_id: "actor-1",
          username: "passkey.user",
        },
        tokens: {
          access_token: "access-token",
          refresh_token: "refresh-token",
          token_type: "Bearer",
          expires_in: 3600,
        },
      }),
      {
        status: 200,
        headers: { "content-type": "application/json" },
      },
    );

    const response = await handleWorkspaceAuthVerifyResponse({
      event,
      workspaceSlug: "alpha",
      upstreamResponse,
    });

    expect(await response.json()).toEqual({
      agent: {
        agent_id: "agent-1",
        actor_id: "actor-1",
        username: "passkey.user",
      },
    });
    expect(recorder.setCalls).toEqual([
      {
        name: "oar_ui_session_alpha",
        value: "refresh-token",
        options: {
          httpOnly: true,
          sameSite: "lax",
          secure: true,
          path: "/",
        },
      },
      {
        name: "oar_ui_access_alpha",
        value: "access-token",
        options: {
          httpOnly: true,
          sameSite: "lax",
          secure: true,
          path: "/",
        },
      },
    ]);
  });
});
