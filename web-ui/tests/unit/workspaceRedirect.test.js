import { describe, expect, it, vi } from "vitest";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceCatalog: vi.fn(),
  resolveWorkspaceInRoute: vi.fn(),
}));

vi.mock("../../src/lib/server/workspaceResolver.js", () => ({
  resolveWorkspaceCatalog: workspaceResolverMocks.resolveWorkspaceCatalog,
  resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
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

    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      workspace: {
        organizationSlug: "local",
        slug: "alpha",
      },
    });

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
});
