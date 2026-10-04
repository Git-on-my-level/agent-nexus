import { afterEach, expect, it, vi } from "vitest";
import { env } from "$env/dynamic/private";
import { createHostedProvider } from "../../src/lib/server/outOfWorkspace/hosted.js";

afterEach(() => {
  delete env.ANX_CONTROL_BASE_URL;
  vi.unstubAllGlobals();
});
function setup(fetchFn) {
  env.ANX_CONTROL_BASE_URL = "https://cp.test";
  const values = new Map([
    ["anx_cp_access_token", "cp-session"],
    ["anx_ui_session_acme__other", "other-refresh"],
  ]);
  const event = {
    fetch: fetchFn,
    locals: {},
    params: {},
    url: new URL("https://ui.test/o/acme/w/alpha"),
    request: new Request("https://ui.test/o/acme/w/alpha"),
    cookies: {
      get: (key) => values.get(key),
      set: vi.fn((key, value) => values.set(key, value)),
      delete: (key) => values.delete(key),
    },
  };
  return {
    event,
    values,
    provider: createHostedProvider({
      controlPlaneBaseUrl: "https://cp.test",
      env,
    }),
  };
}
it("establishes a workspace session without a redirect or exposing grant/tokens to client data", async () => {
  const fetchFn = vi.fn(
    async () =>
      new Response(JSON.stringify({ grant: { bearer_token: "grant-secret" } })),
  );
  const { event, values, provider } = setup(fetchFn);
  const coreFetch = vi.fn(
    async () =>
      new Response(
        JSON.stringify({
          agent: { agent_id: "human" },
          tokens: {
            access_token: "access-secret",
            refresh_token: "refresh-secret",
          },
        }),
      ),
  );
  vi.stubGlobal("fetch", coreFetch);
  const result = await provider.beginLaunchSession({
    event,
    workspaceId: "ws-alpha",
    organizationSlug: "acme",
    workspaceSlug: "alpha",
    returnPath: "/",
  });
  expect(result).toEqual({ kind: "established", agent: { agent_id: "human" } });
  expect(values.get("anx_ui_session_acme__alpha")).toBe("refresh-secret");
  expect(values.get("anx_ui_session_acme__other")).toBe("other-refresh");
  expect(
    new Headers(fetchFn.mock.calls[0][1].headers).get("authorization"),
  ).toBe("Bearer cp-session");
  expect(coreFetch.mock.calls[0][0]).toBe(
    "https://cp.test/ws/acme/alpha/auth/token",
  );
  expect(JSON.parse(coreFetch.mock.calls[0][1].body)).toEqual({
    grant_type: "workspace_human_grant",
    assertion: "grant-secret",
  });
  for (const [, , options] of event.cookies.set.mock.calls)
    expect(options).toMatchObject({
      httpOnly: true,
      sameSite: "lax",
      secure: true,
    });
});
it("does not fall back to launch after access is revoked", async () => {
  const fetchFn = vi.fn(async () => new Response("{}", { status: 403 }));
  const { event, provider } = setup(fetchFn);
  const coreFetch = vi.fn();
  vi.stubGlobal("fetch", coreFetch);
  const result = await provider.beginLaunchSession({
    event,
    workspaceId: "ws-alpha",
    organizationSlug: "acme",
    workspaceSlug: "alpha",
    returnPath: "/",
  });
  expect(result.kind).toBe("needs_signin");
  expect(fetchFn).toHaveBeenCalledTimes(1);
  expect(coreFetch).not.toHaveBeenCalled();
  expect(event.cookies.set).not.toHaveBeenCalled();
});
it("coalesces concurrent catalog reads only within the same request", async () => {
  const fetchFn = vi.fn(
    async (url) =>
      new Response(
        JSON.stringify(
          String(url).includes("/organizations")
            ? { organizations: [{ id: "org", slug: "acme" }] }
            : {
                workspaces: [
                  {
                    id: "ws",
                    organization_id: "org",
                    organization_slug: "acme",
                    slug: "alpha",
                    core_origin: "https://core.test",
                  },
                ],
              },
        ),
      ),
  );
  const { event, provider } = setup(fetchFn);
  const lookup = () =>
    provider.resolveWorkspaceBySlug({
      event,
      organizationSlug: "acme",
      workspaceSlug: "alpha",
    });
  await Promise.all([lookup(), lookup()]);
  expect(fetchFn).toHaveBeenCalledTimes(2);
  event.locals = {};
  await lookup();
  expect(fetchFn).toHaveBeenCalledTimes(4);
});
