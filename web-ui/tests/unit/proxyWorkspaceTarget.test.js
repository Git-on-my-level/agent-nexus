import { describe, expect, it } from "vitest";

import { resolveProxyWorkspaceTarget } from "../../src/lib/server/proxyWorkspaceTarget.js";

function buildCatalog() {
  const defaultWorkspace = {
    slug: "alpha",
    label: "Alpha",
    coreBaseUrl: "http://127.0.0.1:8000/",
  };
  const workspaceBySlug = new Map([[defaultWorkspace.slug, defaultWorkspace]]);
  return {
    defaultWorkspace,
    workspaces: [defaultWorkspace],
    workspaceBySlug,
  };
}

describe("resolveProxyWorkspaceTarget", () => {
  it("rejects proxied requests that omit the workspace header", () => {
    const result = resolveProxyWorkspaceTarget({
      catalog: buildCatalog(),
      workspaceSlug: "",
    });

    expect(result).toMatchObject({
      status: 400,
      payload: {
        error: {
          code: "workspace_header_required",
        },
      },
    });
  });

  it("rejects unknown workspace slugs", () => {
    const result = resolveProxyWorkspaceTarget({
      catalog: buildCatalog(),
      workspaceSlug: "beta",
    });

    expect(result).toMatchObject({
      status: 404,
      payload: {
        error: {
          code: "workspace_not_configured",
        },
      },
    });
  });

  it("returns the normalized upstream for a configured workspace", () => {
    const result = resolveProxyWorkspaceTarget({
      catalog: buildCatalog(),
      workspaceSlug: "alpha",
    });

    expect(result).toMatchObject({
      workspace: {
        slug: "alpha",
      },
      coreBaseUrl: "http://127.0.0.1:8000",
    });
  });
});
