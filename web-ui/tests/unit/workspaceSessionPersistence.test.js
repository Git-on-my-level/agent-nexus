// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

let auth;
let context;
let stop;
beforeEach(async () => {
  vi.resetModules();
  auth = await import("../../src/lib/authSession.js");
  context = await import("../../src/lib/workspaceContext.js");
});
afterEach(() => {
  stop?.();
  stop = null;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
function select(org, ws) {
  context.setCurrentOrganizationSlug(org);
  context.setCurrentWorkspaceSlug(ws);
}
const response = (agent, status = 200) =>
  new Response(JSON.stringify({ agent }), { status });

describe("workspace session persistence", () => {
  it("restores identities independently, including equal slugs in different organizations", () => {
    select("one", "personal");
    auth.completeAuthSession({ agent_id: "one-human" });
    select("two", "personal");
    expect(get(auth.authenticatedAgent)).toBeNull();
    auth.completeAuthSession({ agent_id: "two-human" });
    select("one", "personal");
    expect(get(auth.authSessionReady)).toBe(true);
    expect(get(auth.authenticatedAgent).agent_id).toBe("one-human");
  });
  it("does not publish a late response into the newly selected workspace", async () => {
    select("one", "personal");
    let finish;
    const pending = auth.initializeAuthSession({
      fetchFn: () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    });
    select("one", "omi");
    auth.completeAuthSession({ agent_id: "omi-human" });
    finish(response({ agent_id: "personal-human" }));
    await pending;
    expect(get(auth.authenticatedAgent).agent_id).toBe("omi-human");
    select("one", "personal");
    expect(get(auth.authenticatedAgent).agent_id).toBe("personal-human");
  });
  it("does not resurrect a session after logout while a refresh is pending", async () => {
    select("one", "personal");
    auth.completeAuthSession({ agent_id: "human" });
    let finish;
    const pending = auth.initializeAuthSession({
      fetchFn: () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    });
    auth.clearAuthSession();
    finish(response({ agent_id: "human" }));
    await pending;
    expect(get(auth.authenticatedAgent)).toBeNull();
  });
  it("silently checks visited workspaces with captured headers and clears revocation", async () => {
    vi.useFakeTimers();
    select("one", "personal");
    auth.completeAuthSession({ agent_id: "personal-human" });
    select("two", "omi");
    auth.completeAuthSession({ agent_id: "omi-human" });
    const fetch = vi.fn(async (_url, init) => {
      const headers = new Headers(init.headers);
      if (headers.get("x-anx-workspace-slug") === "personal")
        return response(null, 403);
      return response({ agent_id: "omi-human" });
    });
    vi.stubGlobal("fetch", fetch);
    stop = auth.startWorkspaceSessionMaintenance();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(
      new Headers(fetch.mock.calls[0][1].headers).get(
        "x-anx-organization-slug",
      ),
    ).toBe("one");
    expect(get(auth.authSessionReady)).toBe(true);
    expect(get(auth.authenticatedAgent).agent_id).toBe("omi-human");
    select("one", "personal");
    expect(get(auth.authenticatedAgent)).toBeNull();
  });
});

it("settles an earlier refresh before deleting cookies on logout", async () => {
  select("one", "personal");
  auth.completeAuthSession({ agent_id: "human" });
  let finish;
  const pending = auth.initializeAuthSession({
    fetchFn: () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  });
  const remove = vi.fn(async () => response(null));
  const logout = auth.logoutAuthSession({ fetchFn: remove });
  expect(get(auth.authenticatedAgent)).toBeNull();
  expect(remove).not.toHaveBeenCalled();
  finish(response({ agent_id: "human" }));
  await pending;
  await logout;
  expect(remove).toHaveBeenCalledTimes(1);
  expect(get(auth.authenticatedAgent)).toBeNull();
});

it("discards a refresh when another tab changes the account", async () => {
  let channel;
  vi.stubGlobal(
    "BroadcastChannel",
    class {
      constructor() {
        channel = this;
      }
      postMessage() {}
    },
  );
  vi.resetModules();
  auth = await import("../../src/lib/authSession.js");
  context = await import("../../src/lib/workspaceContext.js");
  select("one", "personal");
  let finish;
  const flight = auth.initializeAuthSession({
    fetchFn: () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  });
  channel.onmessage({ data: "changed" });
  auth.completeAuthSession({ agent_id: "B" });
  finish(response({ agent_id: "A" }));
  await flight;
  expect(get(auth.authenticatedAgent).agent_id).toBe("B");
});
