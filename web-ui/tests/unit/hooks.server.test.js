import { beforeEach, describe, expect, it, vi } from "vitest";

const authSessionState = {
  currentSession: { accessToken: "expired-token" },
};

const authSessionMocks = vi.hoisted(() => ({
  clearWorkspaceAuthSession: vi.fn(),
  getWorkspaceAuthSession: vi.fn(() => authSessionState.currentSession),
  readWorkspaceRefreshToken: vi.fn(() => "refresh-token"),
  refreshWorkspaceAuthSession: vi.fn(async () => {
    authSessionState.currentSession = { accessToken: "fresh-token" };
  }),
}));

vi.mock("$env/dynamic/private", () => ({
  env: {},
}));

vi.mock("$lib/coreRouteCatalog", () => ({
  isProxyableCommand: vi.fn(() => true),
}));

vi.mock("$lib/compat/workspaceCompat", () => ({
  getWorkspaceHeader: vi.fn(() => "ops"),
}));

vi.mock("$lib/workspacePaths", () => ({
  stripBasePath: vi.fn((pathname) => pathname),
}));

vi.mock("$lib/server/authSession", () => ({
  clearWorkspaceAuthSession: authSessionMocks.clearWorkspaceAuthSession,
  getWorkspaceAuthSession: authSessionMocks.getWorkspaceAuthSession,
  readWorkspaceRefreshToken: authSessionMocks.readWorkspaceRefreshToken,
  refreshWorkspaceAuthSession: authSessionMocks.refreshWorkspaceAuthSession,
}));

vi.mock("$lib/server/workspaceCatalog", () => ({
  loadWorkspaceCatalog: vi.fn(() => ({})),
}));

vi.mock("$lib/server/proxyWorkspaceTarget", () => ({
  resolveProxyTarget: vi.fn(() => ({
    coreBaseUrl: "https://core.example.test",
    workspace: { slug: "ops" },
  })),
}));

import { handle } from "../../src/hooks.server.js";

function bodyText(body) {
  if (!body) {
    return "";
  }
  if (body instanceof Uint8Array) {
    return new TextDecoder().decode(body);
  }
  if (body instanceof ArrayBuffer) {
    return new TextDecoder().decode(new Uint8Array(body));
  }
  return String(body);
}

describe("hooks proxy retry", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    authSessionState.currentSession = { accessToken: "expired-token" };
  });

  it("replays the original request body after refreshing workspace auth", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ error: { code: "invalid_token" } }), {
          status: 401,
          headers: {
            "content-type": "application/json",
          },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: {
            "content-type": "application/json",
          },
        }),
      );
    globalThis.fetch = fetchMock;

    const requestBody = JSON.stringify({ action: "update", value: 42 });
    const response = await handle({
      event: {
        url: new URL("https://oar.example.test/api/threads"),
        request: new Request("https://oar.example.test/api/threads", {
          method: "POST",
          headers: {
            accept: "application/json",
            "content-type": "application/json",
          },
          body: requestBody,
        }),
      },
      resolve: vi.fn(),
    });

    expect(response.status).toBe(200);
    expect(fetchMock).toHaveBeenCalledTimes(2);

    const [firstUrl, firstInit] = fetchMock.mock.calls[0];
    const [secondUrl, secondInit] = fetchMock.mock.calls[1];
    expect(firstUrl).toBe("https://core.example.test/api/threads");
    expect(secondUrl).toBe("https://core.example.test/api/threads");
    expect(bodyText(firstInit.body)).toBe(requestBody);
    expect(bodyText(secondInit.body)).toBe(requestBody);
    expect(firstInit.headers.get("authorization")).toBe("Bearer expired-token");
    expect(secondInit.headers.get("authorization")).toBe("Bearer fresh-token");
    expect(authSessionMocks.refreshWorkspaceAuthSession).toHaveBeenCalledTimes(
      1,
    );
  });
});
