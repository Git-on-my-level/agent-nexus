import { describe, expect, it, vi } from "vitest";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
}));

vi.mock("$lib/server/workspaceResolver", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
  };
});

import { load } from "../../src/routes/o/[organization]/w/[workspace]/access/+page.server.js";

describe("access route", () => {
  it("uses the current browser workspace path for copied registration commands", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "scalingforever",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://stale.example.test/anx/o/local/w/scalingforever",
        workspaceId: "ws-scalingforever",
      },
      error: null,
    });

    const result = await load({
      params: {
        organization: "local",
        workspace: "scalingforever",
      },
      url: new URL(
        "https://m2-internal.scalingforever.com/anx/o/local/w/scalingforever/access",
      ),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-scalingforever",
      registrationBaseUrl:
        "https://m2-internal.scalingforever.com/anx/o/local/w/scalingforever",
    });
  });

  it("falls back to the configured public origin when the request origin is loopback", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "scalingforever",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://m2-internal.tail7e1eb.ts.net",
        workspaceId: "ws-scalingforever",
      },
      error: null,
    });

    const result = await load({
      params: {
        organization: "local",
        workspace: "scalingforever",
      },
      url: new URL("http://127.0.0.1:4173/anx/o/local/w/scalingforever/access"),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-scalingforever",
      registrationBaseUrl:
        "https://m2-internal.tail7e1eb.ts.net/anx/o/local/w/scalingforever",
    });
  });

  it("treats bracketed ipv6 loopback as a local request origin", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "scalingforever",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://m2-internal.tail7e1eb.ts.net",
        workspaceId: "ws-scalingforever",
      },
      error: null,
    });

    const result = await load({
      params: {
        organization: "local",
        workspace: "scalingforever",
      },
      url: new URL("http://[::1]:4173/anx/o/local/w/scalingforever/access"),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-scalingforever",
      registrationBaseUrl:
        "https://m2-internal.tail7e1eb.ts.net/anx/o/local/w/scalingforever",
    });
  });
});
