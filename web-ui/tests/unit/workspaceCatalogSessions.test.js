import { describe, expect, it } from "vitest";

import { toPublicWorkspaceCatalog } from "../../src/lib/server/workspaceCatalog.js";

const catalog = {
  defaultWorkspace: { organizationSlug: "acme", slug: "anx" },
  workspaces: [
    {
      organizationSlug: "acme",
      slug: "anx",
      label: "ANX",
      description: "",
    },
    {
      organizationSlug: "acme",
      slug: "beta",
      label: "Beta",
      description: "",
    },
    {
      organizationSlug: "acme",
      slug: "gamma",
      label: "Gamma",
      description: "",
    },
  ],
  devActorMode: false,
};

describe("toPublicWorkspaceCatalog", () => {
  it("calls every workspace readable when nothing scopes sessions", () => {
    // Self-host: one core, one identity, no per-workspace session to miss.
    const published = toPublicWorkspaceCatalog(catalog);
    expect(published.workspaces.map((w) => w.hasSession)).toEqual([
      true,
      true,
      true,
    ]);
  });

  it("marks the workspaces this browser has no session for", () => {
    // Hosted: a session cookie per workspace, written when it is first opened.
    const open = new Set(["anx"]);
    const published = toPublicWorkspaceCatalog(catalog, {
      hasSession: (_org, slug) => open.has(slug),
    });
    expect(published.workspaces.map((w) => [w.slug, w.hasSession])).toEqual([
      ["anx", true],
      ["beta", false],
      ["gamma", false],
    ]);
  });

  it("passes the organization through, so two orgs cannot be confused", () => {
    const seen = [];
    toPublicWorkspaceCatalog(catalog, {
      hasSession: (org, slug) => {
        seen.push(`${org}/${slug}`);
        return true;
      },
    });
    expect(seen).toEqual(["acme/anx", "acme/beta", "acme/gamma"]);
  });

  it("still publishes an empty catalog", () => {
    expect(
      toPublicWorkspaceCatalog({ defaultWorkspace: null, workspaces: [] }),
    ).toMatchObject({ workspaces: [] });
  });
});
