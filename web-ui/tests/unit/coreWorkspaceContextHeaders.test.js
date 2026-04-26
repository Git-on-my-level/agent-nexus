import { describe, expect, it } from "vitest";
import { WORKSPACE_HEADER_CONSTANTS } from "../../src/lib/compat/workspaceCompat.js";
import { buildCoreWorkspaceRoutingHeadersFromSlugs } from "../../src/lib/coreWorkspaceHeadersShared.js";
import { buildServerCoreWorkspaceContextHeaders } from "../../src/lib/server/coreWorkspaceContextHeaders.js";
import { WORKSPACE_HEADER } from "../../src/lib/workspacePaths.js";

describe("core workspace routing headers (shared + server)", () => {
  it("buildCoreWorkspaceRoutingHeadersFromSlugs sets org and workspace header names", () => {
    const h = buildCoreWorkspaceRoutingHeadersFromSlugs({
      organizationSlug: "acme",
      workspaceSlug: "dev",
    });
    expect(h[WORKSPACE_HEADER]).toBe("dev");
    expect(h[WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]).toBe("acme");
  });

  it("omits empty or missing slugs", () => {
    expect(
      buildCoreWorkspaceRoutingHeadersFromSlugs({
        organizationSlug: "  acme  ",
        workspaceSlug: "",
      }),
    ).toEqual({
      [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: "acme",
    });
    expect(
      buildCoreWorkspaceRoutingHeadersFromSlugs({
        organizationSlug: null,
        workspaceSlug: "ws",
      }),
    ).toEqual({
      [WORKSPACE_HEADER]: "ws",
    });
    expect(buildCoreWorkspaceRoutingHeadersFromSlugs({})).toEqual({});
  });

  it("buildServerCoreWorkspaceContextHeaders matches shared output", () => {
    const a = buildServerCoreWorkspaceContextHeaders({
      organizationSlug: "o",
      workspaceSlug: "w",
    });
    const b = buildCoreWorkspaceRoutingHeadersFromSlugs({
      organizationSlug: "o",
      workspaceSlug: "w",
    });
    expect(a).toEqual(b);
  });
});
