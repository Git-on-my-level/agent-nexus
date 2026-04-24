import { describe, expect, it } from "vitest";

import { createLocalProvider } from "../../src/lib/server/outOfWorkspace/local.js";

describe("outOfWorkspace local provider", () => {
  it("returns inert sentinels for every method", async () => {
    const provider = createLocalProvider();

    await expect(
      provider.resolveWorkspaceBySlug({
        event: /** @type {any} */ ({}),
        organizationSlug: "acme",
        workspaceSlug: "alpha",
      }),
    ).resolves.toEqual({ kind: "missing" });
    await expect(
      provider.resolveWorkspaceById({
        event: /** @type {any} */ ({}),
        workspaceId: "ws-1",
      }),
    ).resolves.toEqual({ kind: "missing" });
    await expect(
      provider.listWorkspacesForOrganization({
        event: /** @type {any} */ ({}),
        organizationId: "org-1",
      }),
    ).resolves.toEqual([]);
    await expect(
      provider.beginLaunchSession({
        event: /** @type {any} */ ({}),
        workspaceId: "ws-1",
        returnPath: "/",
      }),
    ).resolves.toEqual({ kind: "workspace_native_login" });
    await expect(
      provider.exchangeLaunchSession({
        event: /** @type {any} */ ({}),
        request: {
          workspaceId: "ws-1",
          exchangeToken: "ex",
          state: "st",
        },
      }),
    ).resolves.toEqual({
      ok: false,
      status: 503,
      code: "control_plane_unavailable",
      message: "Self-hosted workspace has no control plane configured.",
    });

    expect(provider.buildSignInUrl({ workspaceSlug: "alpha" })).toBeNull();
    expect(provider.describeShellCapabilities()).toEqual({
      mode: "local",
      accountPath: null,
      publicOrigin: null,
      allowsEmptyStaticCatalog: false,
    });
  });

  it("proxyHostedApi throws 404 in local mode", async () => {
    const provider = createLocalProvider();
    await expect(
      provider.proxyHostedApi({
        event: /** @type {any} */ ({}),
        method: "GET",
        subpath: "organizations",
      }),
    ).rejects.toMatchObject({ status: 404 });
  });

  it("returns a frozen provider object", async () => {
    const provider = createLocalProvider();
    expect(Object.isFrozen(provider)).toBe(true);
    await provider.resolveWorkspaceBySlug({
      event: /** @type {any} */ ({}),
      organizationSlug: "acme",
      workspaceSlug: "alpha",
    });
    expect(Object.isFrozen(provider)).toBe(true);
  });
});
