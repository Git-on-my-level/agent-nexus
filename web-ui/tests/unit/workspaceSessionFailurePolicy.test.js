/**
 * One rule for every workspace session path: a session is cleared only on an
 * authoritative rejection (401 / 403). Outages (5xx, network errors, timeouts,
 * malformed replies) keep it. See `classifyWorkspaceSessionFailure`.
 */
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  classifyWorkspaceSessionFailure,
  ensureWorkspaceAccessTokenForCoreProxy,
  getAuthAccessCookieName,
  getAuthSessionCookieName,
  loadWorkspaceAuthenticatedAgent,
  resetWorkspaceAuthRefreshStateForTests,
} from "../../src/lib/server/authSession.js";

const ORG = "local";
const WS = "alpha";
const CORE = "https://core.example.com";
const SESSION_COOKIE = getAuthSessionCookieName(ORG, WS);
const ACCESS_COOKIE = getAuthAccessCookieName(ORG, WS);
const RETRY_COOKIE = "anx_ui_auth_retry_local__alpha";

function sessionEvent({ refreshToken = "", accessToken = "" } = {}) {
  const values = new Map();
  const deleted = [];
  if (refreshToken) values.set(SESSION_COOKIE, refreshToken);
  if (accessToken) values.set(ACCESS_COOKIE, accessToken);
  return {
    values,
    deleted,
    event: {
      url: new URL("https://anx.example.com/auth/session"),
      cookies: {
        get: (name) => values.get(name) ?? null,
        set: (name, value) => values.set(name, value),
        delete: (name) => {
          values.delete(name);
          deleted.push(name);
        },
      },
    },
  };
}

function coreReply(status, body) {
  return new Response(
    typeof body === "string" ? body : JSON.stringify(body ?? {}),
    { status, headers: { "content-type": "application/json" } },
  );
}

function errorBody(code) {
  return { error: { code, message: code } };
}

/** Outages: none of these say anything about the session. */
const OUTAGES = [
  ["a 503", () => coreReply(503, errorBody("auth_unavailable"))],
  [
    "a retryable auth-session 503",
    () => coreReply(503, errorBody("auth_session_retryable")),
  ],
  ["a 500", () => coreReply(500, "upstream exploded")],
  ["a 502 from the edge", () => coreReply(502, "<html>Bad gateway</html>")],
  ["a malformed 200", () => coreReply(200, "<html>not json</html>")],
  [
    "a network error",
    () => Promise.reject(new TypeError("fetch failed: ECONNREFUSED")),
  ],
  [
    "a timeout",
    () =>
      Promise.reject(
        Object.assign(new Error("The operation timed out."), {
          name: "TimeoutError",
        }),
      ),
  ],
];

afterEach(() => {
  vi.unstubAllGlobals();
  resetWorkspaceAuthRefreshStateForTests();
});

describe("classifyWorkspaceSessionFailure", () => {
  it.each([
    [{ status: 401, details: errorBody("invalid_token") }, "rejected"],
    [
      { status: 401, details: errorBody("session_ended_by_account_status") },
      "rejected",
    ],
    [{ status: 403, details: errorBody("agent_revoked") }, "rejected"],
    [{ status: 503, code: "auth_session_retryable" }, "unavailable"],
    [{ status: 500 }, "unavailable"],
    [{ status: 502 }, "unavailable"],
    [{ status: 404 }, "unavailable"],
    [new TypeError("fetch failed"), "unavailable"],
    [
      Object.assign(new Error("aborted"), { name: "AbortError" }),
      "unavailable",
    ],
    [undefined, "unavailable"],
  ])("classifies %o as %s", (error, expected) => {
    expect(classifyWorkspaceSessionFailure(error)).toBe(expected);
  });
});

describe("ensureWorkspaceAccessTokenForCoreProxy keeps the session through outages", () => {
  it.each(OUTAGES)("keeps the session on %s", async (_label, reply) => {
    const { event, values, deleted } = sessionEvent({
      refreshToken: "refresh-token",
    });
    vi.stubGlobal("fetch", vi.fn(reply));

    const outcome = await ensureWorkspaceAccessTokenForCoreProxy({
      event,
      organizationSlug: ORG,
      workspaceSlug: WS,
      coreBaseUrl: CORE,
    });

    expect(values.get(SESSION_COOKIE)).toBe("refresh-token");
    expect(deleted).not.toContain(SESSION_COOKIE);
    expect(outcome).toBe("unavailable");
  });

  it("clears the session on a repeated 401 invalid_token", async () => {
    const { event, values, deleted } = sessionEvent({
      refreshToken: "refresh-token",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => coreReply(401, errorBody("invalid_token"))),
    );
    const call = () =>
      ensureWorkspaceAccessTokenForCoreProxy({
        event,
        organizationSlug: ORG,
        workspaceSlug: WS,
        coreBaseUrl: CORE,
      });

    // The first one can be a concurrent refresh rotation: kept once.
    expect(await call()).toBe("unavailable");
    expect(values.get(SESSION_COOKIE)).toBe("refresh-token");
    expect(values.get(RETRY_COOKIE)).toBe("1");

    expect(await call()).toBe("rejected");
    expect(values.get(SESSION_COOKIE)).toBeUndefined();
    expect(deleted).toContain(SESSION_COOKIE);
  });

  it.each([
    [401, "session_ended_by_account_status"],
    [401, "key_mismatch"],
    [403, "agent_revoked"],
  ])(
    "clears the session at once on an authoritative %i %s",
    async (status, code) => {
      const { event, values } = sessionEvent({
        refreshToken: "refresh-token",
      });
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => coreReply(status, errorBody(code))),
      );

      const outcome = await ensureWorkspaceAccessTokenForCoreProxy({
        event,
        organizationSlug: ORG,
        workspaceSlug: WS,
        coreBaseUrl: CORE,
      });

      expect(outcome).toBe("rejected");
      expect(values.get(SESSION_COOKIE)).toBeUndefined();
    },
  );
});

describe("loadWorkspaceAuthenticatedAgent (readOnly) reports outages", () => {
  it.each(OUTAGES.filter(([label]) => label !== "a malformed 200"))(
    "rejects on %s instead of reading as signed out",
    async (_label, reply) => {
      const { event, values, deleted } = sessionEvent({
        accessToken: "access-token",
        refreshToken: "refresh-token",
      });
      vi.stubGlobal("fetch", vi.fn(reply));

      await expect(
        loadWorkspaceAuthenticatedAgent({
          event,
          organizationSlug: ORG,
          workspaceSlug: WS,
          coreBaseUrl: CORE,
          readOnly: true,
        }),
      ).rejects.toBeTruthy();
      expect(values.get(SESSION_COOKIE)).toBe("refresh-token");
      expect(deleted).toEqual([]);
    },
  );

  it("reads a 401 as signed out without touching cookies", async () => {
    const { event, deleted } = sessionEvent({
      accessToken: "access-token",
      refreshToken: "refresh-token",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => coreReply(401, errorBody("invalid_token"))),
    );

    await expect(
      loadWorkspaceAuthenticatedAgent({
        event,
        organizationSlug: ORG,
        workspaceSlug: WS,
        coreBaseUrl: CORE,
        readOnly: true,
      }),
    ).resolves.toBeNull();
    expect(deleted).toEqual([]);
  });
});

describe("loadWorkspaceAuthenticatedAgent (refresh) settles through the same rule", () => {
  it.each(OUTAGES)("keeps the session on %s", async (_label, reply) => {
    const { event, values } = sessionEvent({ refreshToken: "refresh-token" });
    vi.stubGlobal("fetch", vi.fn(reply));

    await expect(
      loadWorkspaceAuthenticatedAgent({
        event,
        organizationSlug: ORG,
        workspaceSlug: WS,
        coreBaseUrl: CORE,
      }),
    ).rejects.toBeTruthy();
    expect(values.get(SESSION_COOKIE)).toBe("refresh-token");
  });

  it("clears the session and reads signed out when the agent was revoked", async () => {
    const { event, values } = sessionEvent({ refreshToken: "refresh-token" });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => coreReply(403, errorBody("agent_revoked"))),
    );

    await expect(
      loadWorkspaceAuthenticatedAgent({
        event,
        organizationSlug: ORG,
        workspaceSlug: WS,
        coreBaseUrl: CORE,
      }),
    ).resolves.toBeNull();
    expect(values.get(SESSION_COOKIE)).toBeUndefined();
  });
});
