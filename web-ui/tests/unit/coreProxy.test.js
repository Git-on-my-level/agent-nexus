import { describe, expect, it } from "vitest";

import { buildProxyRequestInit } from "../../src/lib/server/coreProxy.js";

describe("buildProxyRequestInit", () => {
  it("preserves browser origin and forwards browser host details", () => {
    const event = {
      url: new URL("http://localhost:5173/auth/passkey/register/options"),
      request: new Request(
        "http://localhost:5173/auth/passkey/register/options",
        {
          method: "POST",
          headers: {
            origin: "http://localhost:5173",
            host: "localhost:5173",
            "content-type": "application/json",
            cookie: "oar_ui_session_local=refresh-token",
            authorization: "Bearer token",
          },
          body: JSON.stringify({ display_name: "Alex Chen" }),
        },
      ),
    };

    const requestInit = buildProxyRequestInit(event);

    expect(requestInit.method).toBe("POST");
    expect(requestInit.duplex).toBe("half");
    expect(requestInit.headers.get("origin")).toBe("http://localhost:5173");
    expect(requestInit.headers.get("x-forwarded-host")).toBe("localhost:5173");
    expect(requestInit.headers.get("x-forwarded-proto")).toBe("http");
    expect(requestInit.headers.get("host")).toBeNull();
    expect(requestInit.headers.get("cookie")).toBeNull();
    expect(requestInit.headers.get("authorization")).toBeNull();
  });

  it("omits body for GET requests", () => {
    const event = {
      url: new URL("https://oar.example.com/meta/handshake"),
      request: new Request("https://oar.example.com/meta/handshake", {
        method: "GET",
      }),
    };

    const requestInit = buildProxyRequestInit(event);

    expect(requestInit.method).toBe("GET");
    expect("body" in requestInit).toBe(false);
    expect("duplex" in requestInit).toBe(false);
    expect(requestInit.headers.get("x-forwarded-host")).toBe("oar.example.com");
    expect(requestInit.headers.get("x-forwarded-proto")).toBe("https");
  });
});
