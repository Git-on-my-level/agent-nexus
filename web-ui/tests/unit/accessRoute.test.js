import { describe, expect, it, vi } from "vitest";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceBySlug: vi.fn(),
}));

vi.mock("$lib/server/workspaceResolver", () => ({
  resolveWorkspaceBySlug: workspaceResolverMocks.resolveWorkspaceBySlug,
}));

import { load } from "../../src/routes/[workspace]/access/+page.server.js";

describe("access route", () => {
  it("uses the current browser workspace path for copied registration commands", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
      workspaceSlug: "scalingforever",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://stale.example.test/anx/scalingforever",
        workspaceId: "ws-scalingforever",
      },
    });

    const result = await load({
      params: {
        workspace: "scalingforever",
      },
      url: new URL(
        "https://m2-internal.scalingforever.com/anx/scalingforever/access",
      ),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-scalingforever",
      registrationBaseUrl:
        "https://m2-internal.scalingforever.com/anx/scalingforever",
    });
  });

  it("falls back to the configured public origin when the request origin is loopback", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
      workspaceSlug: "scalingforever",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://m2-internal.tail7e1eb.ts.net",
        workspaceId: "ws-scalingforever",
      },
    });

    const result = await load({
      params: {
        workspace: "scalingforever",
      },
      url: new URL("http://127.0.0.1:4173/anx/scalingforever/access"),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-scalingforever",
      registrationBaseUrl:
        "https://m2-internal.tail7e1eb.ts.net/anx/scalingforever",
    });
  });

  it("treats bracketed ipv6 loopback as a local request origin", async () => {
    workspaceResolverMocks.resolveWorkspaceBySlug.mockResolvedValue({
      workspaceSlug: "scalingforever",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://m2-internal.tail7e1eb.ts.net",
        workspaceId: "ws-scalingforever",
      },
    });

    const result = await load({
      params: {
        workspace: "scalingforever",
      },
      url: new URL("http://[::1]:4173/anx/scalingforever/access"),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-scalingforever",
      registrationBaseUrl:
        "https://m2-internal.tail7e1eb.ts.net/anx/scalingforever",
    });
  });
});
