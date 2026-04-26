import { describe, expect, it } from "vitest";

import { parseWorkspaceRouteSlugs } from "../../src/lib/workspacePaths.js";

describe("parseWorkspaceRouteSlugs (hosted /ws and UI /o/.../w/...)", () => {
  it("parses /o/{org}/w/{workspace} and nested subpaths (UI shell)", () => {
    expect(parseWorkspaceRouteSlugs("/o/acme-corp/w/alpha")).toEqual({
      organizationSlug: "acme-corp",
      workspaceSlug: "alpha",
    });
    expect(
      parseWorkspaceRouteSlugs("/o/acme-corp/w/alpha/inbox", ""),
    ).toEqual({
      organizationSlug: "acme-corp",
      workspaceSlug: "alpha",
    });
  });

  it("parses /ws/{org}/{workspace} and nested API subpaths (hosted proxy path shape)", () => {
    expect(parseWorkspaceRouteSlugs("/ws/scaling-forever/personal")).toEqual({
      organizationSlug: "scaling-forever",
      workspaceSlug: "personal",
    });
    expect(
      parseWorkspaceRouteSlugs(
        "/ws/scaling-forever/personal/auth/token",
        "",
      ),
    ).toEqual({
      organizationSlug: "scaling-forever",
      workspaceSlug: "personal",
    });
  });

  it("parses stream-style paths: URL segments parse, but the control plane may still reject the route", () => {
    // The web-ui path parser is structural only; it treats everything after
    // /ws/{org}/{ws}/ as an opaque subpath. Core SSE paths like
    // /events/stream are not necessarily supported through the CP workspace
    // proxy — that is a separate product/contract check.
    expect(
      parseWorkspaceRouteSlugs(
        "/ws/scaling-forever/personal/events/stream",
        "",
      ),
    ).toEqual({
      organizationSlug: "scaling-forever",
      workspaceSlug: "personal",
    });
  });

  it("strips a configured app base before matching (e.g. packed/mounted base path)", () => {
    expect(
      parseWorkspaceRouteSlugs(
        "/app/ws/my-org/my-ws/threads",
        "/app",
      ),
    ).toEqual({
      organizationSlug: "my-org",
      workspaceSlug: "my-ws",
    });
  });

  it("returns empty slugs for paths that are not org-scoped workspace routes", () => {
    const empty = { organizationSlug: "", workspaceSlug: "" };

    expect(parseWorkspaceRouteSlugs("/")).toEqual(empty);
    expect(parseWorkspaceRouteSlugs("/ws")).toEqual(empty);
    expect(parseWorkspaceRouteSlugs("/ws/")).toEqual(empty);
    expect(parseWorkspaceRouteSlugs("/ws/only-one-segment")).toEqual(empty);
    expect(
      parseWorkspaceRouteSlugs("/o/missing-w-segment/alpha"),
    ).toEqual(empty);
    expect(parseWorkspaceRouteSlugs("/o/acme/w")).toEqual(empty);
  });
});
