import { expect, it, vi } from "vitest";
import { scopeWorkspaceCookies } from "../../src/lib/server/sessionAdapter.js";
import {
  getWorkspaceAuthSession,
  loadWorkspaceAuthenticatedAgent,
  writeWorkspaceAccessToken,
  writeWorkspaceRefreshToken,
  refreshWorkspaceAuthSession,
} from "../../src/lib/server/authSession.js";

function request(jar, scope) {
  const snapshot = new Map(jar);
  const writes = [];
  const event = {
    locals: {},
    url: new URL("https://ui.test"),
    cookies: {
      get: (key) => snapshot.get(key),
      getAll: () => [...snapshot].map(([name, value]) => ({ name, value })),
      set: (key, value) => {
        snapshot.set(key, value);
        writes.push([key, value]);
      },
      delete: (key) => {
        snapshot.delete(key);
        writes.push([key, undefined]);
      },
    },
  };
  scopeWorkspaceCookies(event, scope.repeat(64));
  return {
    event,
    deliver() {
      for (const [key, value] of writes) {
        if (value === undefined) jar.delete(key);
        else jar.set(key, value);
      }
    },
  };
}
function issue(event, owner) {
  writeWorkspaceAccessToken(event, "org", "personal", `${owner}-access`);
  writeWorkspaceRefreshToken(event, "org", "personal", `${owner}-refresh`);
}
it("late issuance after logout/login cannot restore another account's session, even in the same organization", () => {
  const jar = new Map();
  const a = request(jar, "a");
  issue(a.event, "A"); // pause response AFTER core issuance
  jar.clear(); // logout
  const b = request(jar, "b");
  issue(b.event, "B");
  b.deliver(); // login B, same org + workspace
  a.deliver(); // release old response into the actual browser cookie jar
  expect(
    getWorkspaceAuthSession(request(jar, "b").event, "org", "personal"),
  ).toEqual({ accessToken: "B-access", refreshToken: "B-refresh" });
  // Before B establishes a session, neither the late A nor legacy cookies work.
  jar.set("anx_ui_access_org__personal", "A-access");
  expect(
    getWorkspaceAuthSession(request(jar, "c").event, "org", "personal"),
  ).toBeNull();
});
it("a refresh started in another tab stays bound to its original login after delayed completion", async () => {
  const jar = new Map();
  const a = request(jar, "a");
  issue(a.event, "A");
  a.deliver();
  const tab = request(jar, "a");
  let release;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    ),
  );
  try {
    const flight = refreshWorkspaceAuthSession({
      event: tab.event,
      organizationSlug: "org",
      workspaceSlug: "personal",
      coreBaseUrl: "https://core.test",
    });
    jar.clear();
    const b = request(jar, "b");
    issue(b.event, "B");
    b.deliver();
    release(
      new Response(
        JSON.stringify({
          tokens: {
            access_token: "A-new-access",
            refresh_token: "A-new-refresh",
          },
        }),
      ),
    );
    await flight;
    tab.deliver();
    expect(
      getWorkspaceAuthSession(request(jar, "b").event, "org", "personal")
        .accessToken,
    ).toBe("B-access");
  } finally {
    vi.unstubAllGlobals();
  }
});

it("speculative validation never refreshes or mutates cookies after an expired access token", async () => {
  const jar = new Map();
  const setup = request(jar, "a");
  issue(setup.event, "A");
  setup.deliver();
  const before = new Map(jar);
  const preload = request(jar, "a");
  const fetch = vi.fn(async () => new Response("{}", { status: 401 }));
  vi.stubGlobal("fetch", fetch);
  try {
    expect(
      await loadWorkspaceAuthenticatedAgent({
        event: preload.event,
        organizationSlug: "org",
        workspaceSlug: "personal",
        coreBaseUrl: "https://core.test",
        readOnly: true,
      }),
    ).toBeNull();
    preload.deliver();
    expect(jar).toEqual(before);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe("https://core.test/agents/me");
    expect(fetch.mock.calls[0][1].method).toBe("GET");
  } finally {
    vi.unstubAllGlobals();
  }
});
