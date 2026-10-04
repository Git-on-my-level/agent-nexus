import { beforeEach, expect, it, vi } from "vitest";
const resolve = vi.hoisted(() => vi.fn());
vi.mock("$lib/server/workspaceResolver.js", () => ({
  resolveWorkspaceInRoute: resolve,
}));
import { POST } from "../../src/routes/auth/workspace-session/+server.js";
import { scopeWorkspaceCookies } from "../../src/lib/server/sessionAdapter.js";
const workspace = {
  organizationSlug: "org",
  slug: "personal",
  id: "workspace",
};
beforeEach(() => {
  resolve.mockReset();
  resolve.mockResolvedValue({ workspace });
});
function event(origin = "https://ui.test") {
  const jar = new Map();
  const establishSession = vi.fn(async () => ({
    agent: { agent_id: "human" },
    tokens: { access_token: "access", refresh_token: "refresh" },
  }));
  const e = {
    locals: {
      sessionAdapter: {
        sessionTarget: () => ({ coreBaseUrl: "https://core.test" }),
        establishSession,
      },
    },
    url: new URL("https://ui.test/auth/workspace-session"),
    request: new Request("https://ui.test/auth/workspace-session", {
      method: "POST",
      headers: { origin, "content-type": "application/json" },
      body: JSON.stringify({
        organizationSlug: "org",
        workspaceSlug: "personal",
      }),
    }),
    cookies: {
      get: (k) => jar.get(k),
      getAll: () => [],
      set: vi.fn((k, v) => jar.set(k, v)),
      delete: vi.fn((k) => jar.delete(k)),
    },
  };
  scopeWorkspaceCookies(e, "a".repeat(64));
  return { e, jar, establishSession };
}
it("only explicit same-origin selection establishes and remembers a workspace", async () => {
  const { e, jar, establishSession } = event();
  const response = await POST(e);
  expect(response.status).toBe(200);
  expect(await response.json()).toEqual({ agent: { agent_id: "human" } });
  expect(establishSession).toHaveBeenCalledWith(workspace);
  expect(
    [...jar.keys()].some((k) =>
      k.startsWith("anx_ui_access_org__personal__s_"),
    ),
  ).toBe(true);
  expect(response.headers.get("cache-control")).toBe("private, no-store");
});
it("rejects CSRF before catalog access or session issuance", async () => {
  const { e, jar, establishSession } = event("https://other.test");
  expect((await POST(e)).status).toBe(403);
  expect(resolve).not.toHaveBeenCalled();
  expect(establishSession).not.toHaveBeenCalled();
  expect(jar.size).toBe(0);
});
it("revoked membership cannot establish, refresh, or set last-workspace", async () => {
  resolve.mockResolvedValue({
    error: { status: 403, payload: { error: "revoked" } },
  });
  const { e, jar, establishSession } = event();
  expect((await POST(e)).status).toBe(403);
  expect(establishSession).not.toHaveBeenCalled();
  expect(jar.size).toBe(0);
});
