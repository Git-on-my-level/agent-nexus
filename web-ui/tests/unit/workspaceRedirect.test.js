import { describe, expect, it, vi } from "vitest";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceCatalog: vi.fn(),
}));

vi.mock("../../src/lib/server/workspaceResolver.js", () => ({
  resolveWorkspaceCatalog: workspaceResolverMocks.resolveWorkspaceCatalog,
}));

import { redirectToDefaultWorkspace } from "../../src/lib/server/workspaceRedirect.js";

describe("redirectToDefaultWorkspace", () => {
  it("redirects unprefixed routes through the resolved default workspace", async () => {
    const event = {
      cookies: {
        get() {
          return null;
        },
      },
    };

    workspaceResolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      defaultWorkspace: {
        slug: "alpha",
      },
    });

    await expect(
      redirectToDefaultWorkspace(event, "/threads/thread-onboarding"),
    ).rejects.toMatchObject({
      status: 307,
      location: "/alpha/threads/thread-onboarding",
    });

    expect(workspaceResolverMocks.resolveWorkspaceCatalog).toHaveBeenCalledWith(
      event,
    );
  });
});
