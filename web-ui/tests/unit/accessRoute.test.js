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
  it("uses workspace coreBaseUrl for copied anx CLI --base-url (API origin)", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "ops",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://stale.example.test/anx/o/local/w/ops",
        workspaceId: "ws-ops",
      },
      error: null,
    });

    const result = await load({
      params: {
        organization: "local",
        workspace: "ops",
      },
      url: new URL("https://workspace.example.com/anx/o/local/w/ops/access"),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-ops",
      cliBaseUrl: "http://127.0.0.1:8002",
      outOfWorkspaceMode: "local",
    });
  });

  it("uses coreBaseUrl over public origin when the request origin is loopback", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "ops",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://workspace.example.test",
        workspaceId: "ws-ops",
      },
      error: null,
    });

    const result = await load({
      params: {
        organization: "local",
        workspace: "ops",
      },
      url: new URL("http://127.0.0.1:4173/anx/o/local/w/ops/access"),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-ops",
      cliBaseUrl: "http://127.0.0.1:8002",
      outOfWorkspaceMode: "local",
    });
  });

  it("treats bracketed ipv6 loopback as a local request origin and still prefers coreBaseUrl", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "ops",
      workspace: {
        coreBaseUrl: "http://127.0.0.1:8002",
        publicOrigin: "https://workspace.example.test",
        workspaceId: "ws-ops",
      },
      error: null,
    });

    const result = await load({
      params: {
        organization: "local",
        workspace: "ops",
      },
      url: new URL("http://[::1]:4173/anx/o/local/w/ops/access"),
    });

    expect(result).toEqual({
      coreBaseUrl: "http://127.0.0.1:8002",
      workspaceId: "ws-ops",
      cliBaseUrl: "http://127.0.0.1:8002",
      outOfWorkspaceMode: "local",
    });
  });

  it("falls back to public workspace URL when coreBaseUrl is not configured", async () => {
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      organizationSlug: "local",
      workspaceSlug: "ops",
      workspace: {
        coreBaseUrl: "",
        publicOrigin: "https://workspace.example.test",
        workspaceId: "ws-ops",
      },
      error: null,
    });

    const result = await load({
      params: {
        organization: "local",
        workspace: "ops",
      },
      url: new URL("http://127.0.0.1:4173/anx/o/local/w/ops/access"),
    });

    expect(result).toEqual({
      coreBaseUrl: "",
      workspaceId: "ws-ops",
      cliBaseUrl: "https://workspace.example.test/anx/o/local/w/ops",
      outOfWorkspaceMode: "local",
    });
  });
});
