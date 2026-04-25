import { describe, expect, it, vi } from "vitest";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceCatalog: vi.fn(),
  resolveWorkspaceInRoute: vi.fn(),
}));

const oowMocks = vi.hoisted(() => ({
  getOutOfWorkspaceProvider: vi.fn(),
}));

vi.mock("../../src/lib/server/workspaceResolver.js", () => ({
  resolveWorkspaceCatalog: workspaceResolverMocks.resolveWorkspaceCatalog,
  resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
}));

vi.mock("../../src/lib/server/outOfWorkspace/index.js", () => ({
  getOutOfWorkspaceProvider: oowMocks.getOutOfWorkspaceProvider,
  __resetOutOfWorkspaceProviderCacheForTests: () => {},
}));

import { redirectToRecentWorkspaceOrChooser } from "../../src/lib/server/workspaceRedirect.js";

describe("redirectToRecentWorkspaceOrChooser", () => {
  it("redirects through a last-workspace cookie when it resolves", async () => {
    const event = {
      cookies: {
        get(name) {
          return name === "anx_last_workspace" ? "local:alpha" : null;
        },
      },
    };

    workspaceResolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      defaultWorkspace: { organizationSlug: "local", slug: "local" },
    });
    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      workspace: {
        organizationSlug: "local",
        slug: "alpha",
      },
    });
    oowMocks.getOutOfWorkspaceProvider.mockReturnValue({ mode: "local" });

    await expect(
      redirectToRecentWorkspaceOrChooser(event, "/threads/thread-onboarding"),
    ).rejects.toMatchObject({
      status: 307,
      location: "/o/local/w/alpha/threads/thread-onboarding",
    });

    expect(workspaceResolverMocks.resolveWorkspaceInRoute).toHaveBeenCalledWith(
      {
        event,
        organizationSlug: "local",
        workspaceSlug: "alpha",
      },
    );
  });

  it("redirects to the default self-host workspace when local and no valid cookie", async () => {
    const event = { cookies: { get: () => null } };
    workspaceResolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      defaultWorkspace: { organizationSlug: "local", slug: "local" },
    });
    oowMocks.getOutOfWorkspaceProvider.mockReturnValue({ mode: "local" });

    await expect(
      redirectToRecentWorkspaceOrChooser(event, ""),
    ).rejects.toMatchObject({
      status: 307,
      location: "/o/local/w/local",
    });
  });

  it("redirects to /hosted/start when the provider is hosted", async () => {
    const event = { cookies: { get: () => null } };
    workspaceResolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      defaultWorkspace: { organizationSlug: "local", slug: "local" },
    });
    oowMocks.getOutOfWorkspaceProvider.mockReturnValue({ mode: "hosted" });

    await expect(
      redirectToRecentWorkspaceOrChooser(event, ""),
    ).rejects.toMatchObject({
      status: 307,
      location: "/hosted/start",
    });
  });

  it("redirects to /hosted/start when local but no default workspace is configured", async () => {
    const event = { cookies: { get: () => null } };
    workspaceResolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      defaultWorkspace: null,
    });
    oowMocks.getOutOfWorkspaceProvider.mockReturnValue({ mode: "local" });

    await expect(
      redirectToRecentWorkspaceOrChooser(event, ""),
    ).rejects.toMatchObject({
      status: 307,
      location: "/hosted/start",
    });
  });
});
