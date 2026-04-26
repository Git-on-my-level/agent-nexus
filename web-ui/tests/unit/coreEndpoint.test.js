import { describe, expect, it } from "vitest";
import { coreEndpointURL } from "../../src/lib/server/coreEndpoint.js";

describe("coreEndpointURL (hosted base path preservation)", () => {
  const hostedBase = "http://localhost:5173/ws/scaling-forever/personal";

  it("joins /auth/token without dropping /ws/{org}/{workspace}", () => {
    expect(coreEndpointURL(hostedBase, "/auth/token")).toBe(
      "http://localhost:5173/ws/scaling-forever/personal/auth/token",
    );
  });

  it("joins /agents/me without dropping the hosted prefix", () => {
    expect(coreEndpointURL(hostedBase, "/agents/me")).toBe(
      "http://localhost:5173/ws/scaling-forever/personal/agents/me",
    );
  });

  it("joins /auth/passkey/dev/login without dropping the hosted prefix", () => {
    expect(coreEndpointURL(hostedBase, "/auth/passkey/dev/login")).toBe(
      "http://localhost:5173/ws/scaling-forever/personal/auth/passkey/dev/login",
    );
  });

  it("normalizes trailing base slashes and leading path slashes", () => {
    expect(coreEndpointURL(`${hostedBase}/`, "auth/token")).toBe(
      "http://localhost:5173/ws/scaling-forever/personal/auth/token",
    );
  });
});
