import { describe, expect, it } from "vitest";

import { createOarCoreClient } from "../../src/lib/oarCoreClient.js";

describe("oarCoreClient error messaging", () => {
  it("returns actionable guidance when core is unreachable", async () => {
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async () => {
        throw new TypeError("fetch failed");
      },
    });

    await expect(client.listActors()).rejects.toThrow(
      /Unable to reach oar-core at http:\/\/core\.test[\s\S]*Check that oar-core is running and OAR_CORE_BASE_URL is correct\./,
    );
  });

  it("extracts nested JSON error messages from non-2xx responses", async () => {
    const client = createOarCoreClient({
      baseUrl: "http://core.test",
      fetchFn: async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "core_unreachable",
              message: "backend unavailable",
            },
          }),
          {
            status: 503,
            statusText: "Service Unavailable",
            headers: { "content-type": "application/json" },
          },
        ),
    });

    await expect(client.listActors()).rejects.toThrow(
      /backend unavailable[\s\S]*oar-core may be unavailable; verify backend startup and base URL\./,
    );
  });
});
