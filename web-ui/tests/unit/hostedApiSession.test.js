import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.resetModules();
});

async function loadSessionHandlers(dev) {
  vi.doMock("$app/environment", () => ({
    dev,
    browser: false,
  }));
  return import("../../src/routes/hosted/api/session/+server.js");
}

describe("hosted /api/session", () => {
  it("POST sets HttpOnly cookie when not in dev", async () => {
    const { POST } = await loadSessionHandlers(false);
    const cookies = {
      set: vi.fn(),
      delete: vi.fn(),
    };
    const event = {
      request: new Request("https://app.example/hosted/api/session", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ access_token: "cp_secret" }),
      }),
      url: new URL("https://app.example/hosted/api/session"),
      cookies,
    };
    const res = await POST(event);
    expect(res.status).toBe(200);
    expect(cookies.set).toHaveBeenCalledWith(
      "anx_cp_access_token",
      "cp_secret",
      expect.objectContaining({
        path: "/",
        httpOnly: true,
        sameSite: "lax",
        secure: true,
      }),
    );
    expect(cookies.delete).toHaveBeenCalledWith("anx_cp_dev_access_token", {
      path: "/",
    });
  });

  it("POST sets Secure on http when not in dev (TLS-termination / internal http hop)", async () => {
    const { POST } = await loadSessionHandlers(false);
    const cookies = { set: vi.fn(), delete: vi.fn() };
    const event = {
      request: new Request("http://127.0.0.1:5173/hosted/api/session", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ access_token: "tok" }),
      }),
      url: new URL("http://127.0.0.1:5173/hosted/api/session"),
      cookies,
    };
    await POST(event);
    expect(cookies.set).toHaveBeenCalledWith(
      "anx_cp_access_token",
      "tok",
      expect.objectContaining({ secure: true }),
    );
  });

  it("POST rejects empty body token", async () => {
    const { POST } = await loadSessionHandlers(false);
    const event = {
      request: new Request("https://app.example/hosted/api/session", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ access_token: "  " }),
      }),
      url: new URL("https://app.example/hosted/api/session"),
      cookies: { set: vi.fn(), delete: vi.fn() },
    };
    const res = await POST(event);
    expect(res.status).toBe(400);
  });

  it("POST is disabled in dev", async () => {
    const { POST } = await loadSessionHandlers(true);
    const event = {
      request: new Request("http://127.0.0.1/hosted/api/session", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ access_token: "x" }),
      }),
      url: new URL("http://127.0.0.1/hosted/api/session"),
      cookies: { set: vi.fn(), delete: vi.fn() },
    };
    const res = await POST(event);
    expect(res.status).toBe(403);
    expect(event.cookies.set).not.toHaveBeenCalled();
  });

  it("DELETE clears both cookie names", async () => {
    const { DELETE } = await loadSessionHandlers(false);
    const cookies = { delete: vi.fn() };
    const event = {
      request: new Request("https://app.example/hosted/api/session", {
        method: "DELETE",
      }),
      url: new URL("https://app.example/hosted/api/session"),
      cookies,
    };
    const res = await DELETE(event);
    expect(res.status).toBe(200);
    expect(cookies.delete).toHaveBeenCalledWith("anx_cp_access_token", {
      path: "/",
    });
    expect(cookies.delete).toHaveBeenCalledWith("anx_cp_dev_access_token", {
      path: "/",
    });
  });
});
