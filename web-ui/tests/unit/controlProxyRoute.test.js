import { beforeEach, describe, expect, it, vi } from "vitest";

const controlClientMocks = vi.hoisted(() => ({
  getControlBaseUrl: vi.fn(() => "https://control.example.test/api"),
}));

const controlSessionMocks = vi.hoisted(() => ({
  readControlAccessToken: vi.fn(() => "control-token"),
}));

vi.mock("$lib/server/controlClient.js", () => ({
  getControlBaseUrl: controlClientMocks.getControlBaseUrl,
}));

vi.mock("$lib/server/controlSession.js", () => ({
  readControlAccessToken: controlSessionMocks.readControlAccessToken,
}));

import { POST } from "../../src/routes/control/api/[...segments]/+server.js";

describe("control proxy route", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    controlClientMocks.getControlBaseUrl.mockReturnValue(
      "https://control.example.test/api",
    );
    controlSessionMocks.readControlAccessToken.mockReturnValue("control-token");
  });

  it("rejects absolute proxy targets before forwarding auth headers", async () => {
    globalThis.fetch = vi.fn();

    const response = await POST({
      params: {
        segments: "https://attacker.example/steal",
      },
      request: new Request(
        "https://oar.example.test/control/api/https://attacker.example/steal",
        {
          method: "POST",
          headers: {
            "content-type": "application/json",
          },
          body: JSON.stringify({ ok: true }),
        },
      ),
      url: new URL(
        "https://oar.example.test/control/api/https://attacker.example/steal",
      ),
    });

    expect(response.status).toBe(400);
    expect(await response.json()).toEqual({
      error: {
        code: "invalid_request",
        message: "Control API path must be relative.",
      },
    });
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("keeps proxied requests pinned to the control origin", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: {
          "content-type": "application/json",
        },
      }),
    );

    const response = await POST({
      params: {
        segments: "organizations/org-1/invites",
      },
      request: new Request(
        "https://oar.example.test/control/api/organizations/org-1/invites?limit=1",
        {
          method: "POST",
          headers: {
            "content-type": "application/json",
          },
          body: JSON.stringify({ role: "member" }),
        },
      ),
      url: new URL(
        "https://oar.example.test/control/api/organizations/org-1/invites?limit=1",
      ),
    });

    expect(response.status).toBe(200);
    const [targetUrl, init] = globalThis.fetch.mock.calls[0];
    expect(String(targetUrl)).toBe(
      "https://control.example.test/api/organizations/org-1/invites?limit=1",
    );
    expect(init.headers.get("authorization")).toBe("Bearer control-token");
  });
});
