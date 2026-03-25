import { afterEach, describe, expect, it, vi } from "vitest";

async function loadMockGuard(coreBaseUrl = "") {
  vi.resetModules();
  vi.doMock("$env/dynamic/private", () => ({
    env: {
      OAR_CORE_BASE_URL: coreBaseUrl,
    },
  }));
  return import("../../src/lib/server/mockGuard.js");
}

afterEach(() => {
  vi.resetModules();
  vi.doUnmock("$env/dynamic/private");
});

describe("mockGuard", () => {
  it("parses JSON request bodies and returns a stable 400 response on invalid JSON", async () => {
    const { readMockJsonBody } = await loadMockGuard();

    const valid = await readMockJsonBody(
      new Request("http://oar.test/mock", {
        method: "POST",
        body: JSON.stringify({ actor_id: "actor-1" }),
        headers: { "content-type": "application/json" },
      }),
    );
    expect(valid).toEqual({
      ok: true,
      body: { actor_id: "actor-1" },
    });

    const invalid = await readMockJsonBody(
      new Request("http://oar.test/mock", {
        method: "POST",
        body: "{bad",
        headers: { "content-type": "application/json" },
      }),
    );
    expect(invalid.ok).toBe(false);
    expect(invalid.response.status).toBe(400);
    await expect(invalid.response.json()).resolves.toEqual({
      error: "Invalid JSON body.",
    });
  });

  it("exposes the clearer mock-mode guard name while preserving the legacy alias", async () => {
    const { assertMockModeEnabled, guardMockRoute } =
      await loadMockGuard("http://core.test/");

    const direct = assertMockModeEnabled("/threads");
    const legacy = guardMockRoute("/threads");

    expect(direct.status).toBe(500);
    expect(await direct.json()).toMatchObject({
      error: {
        code: "mock_route_disabled",
      },
    });
    expect(await legacy.json()).toMatchObject({
      error: {
        code: "mock_route_disabled",
      },
    });
  });
});
