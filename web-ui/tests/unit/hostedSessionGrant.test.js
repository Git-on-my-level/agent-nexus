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
