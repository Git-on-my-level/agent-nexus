import { describe, expect, it } from "vitest";

import { WORKSPACE_HEADER_CONSTANTS } from "../../src/lib/compat/workspaceCompat";
import { buildCoreRequestContextHeaders } from "../../src/lib/coreClientRequestHeaders";
import { WORKSPACE_HEADER } from "../../src/lib/workspacePaths";

describe("buildCoreRequestContextHeaders", () => {
  it("prefers store when both slugs are set", () => {
    expect(
      buildCoreRequestContextHeaders({
        storeOrg: "acme",
        storeWorkspace: "prod",
        pathname: "/o/other/w/ws2/topics",
        basePath: "",
      }),
    ).toEqual({
      [WORKSPACE_HEADER]: "prod",
      [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: "acme",
    });
  });

  it("fills missing slugs from /o/{org}/w/{ws}/ URL", () => {
    expect(
      buildCoreRequestContextHeaders({
        storeOrg: "",
        storeWorkspace: "",
        pathname: "/o/x/w/y/threads",
        basePath: "",
      }),
    ).toEqual({
      [WORKSPACE_HEADER]: "y",
      [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: "x",
    });
  });

  it("maps org from first segment and workspace from second per route convention", () => {
    expect(
      buildCoreRequestContextHeaders({
        storeOrg: "",
        storeWorkspace: "",
        pathname: "/o/x/w/y",
        basePath: "",
      }),
    ).toEqual({
      [WORKSPACE_HEADER]: "y",
      [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: "x",
    });
  });

  it("returns no headers when store is empty and pathname is not workspace-scoped", () => {
    expect(
      buildCoreRequestContextHeaders({
        storeOrg: "",
        storeWorkspace: "",
        pathname: "/auth/login",
        basePath: "",
      }),
    ).toEqual({});
  });

  it("keeps store org when URL names a different organization", () => {
    expect(
      buildCoreRequestContextHeaders({
        storeOrg: "stored-org",
        storeWorkspace: "",
        pathname: "/o/url-org/w/url-ws/inbox",
        basePath: "",
      }),
    ).toEqual({
      [WORKSPACE_HEADER]: "url-ws",
      [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: "stored-org",
    });
  });

  it("keeps store workspace when URL names a different org segment", () => {
    expect(
      buildCoreRequestContextHeaders({
        storeOrg: "",
        storeWorkspace: "stored-ws",
        pathname: "/o/url-org/w/url-ws/topics",
        basePath: "",
      }),
    ).toEqual({
      [WORKSPACE_HEADER]: "stored-ws",
      [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: "url-org",
    });
  });

  it("strips base path before parsing workspace route", () => {
    expect(
      buildCoreRequestContextHeaders({
        storeOrg: "",
        storeWorkspace: "",
        pathname: "/anx/o/alpha/w/beta/docs",
        basePath: "/anx",
      }),
    ).toEqual({
      [WORKSPACE_HEADER]: "beta",
      [WORKSPACE_HEADER_CONSTANTS.ORGANIZATION_HEADER]: "alpha",
    });
  });

  it("omits headers for empty trimmed store values without a URL match", () => {
    expect(
      buildCoreRequestContextHeaders({
        storeOrg: "   ",
        storeWorkspace: "  ",
        pathname: "/",
        basePath: "",
      }),
    ).toEqual({});
  });
});
