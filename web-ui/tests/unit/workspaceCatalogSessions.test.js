import { describe, expect, it } from "vitest";

import {
  toPublicWorkspaceCatalog,
  workspaceSessionProbe,
} from "../../src/lib/server/workspaceCatalog.js";

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

describe("workspaceSessionProbe", () => {
  const event = (cookieNames) => ({
    cookies: {
      get: (name) => (cookieNames.includes(name) ? "token" : undefined),
    },
  });

  it("asks nothing on a shell that does not scope sessions per workspace", () => {
    expect(workspaceSessionProbe(event([]), { mode: "local" })).toBeUndefined();
    expect(workspaceSessionProbe(event([]), {})).toBeUndefined();
  });

  it("reads the per-workspace cookie by name", () => {
    const probe = workspaceSessionProbe(event(["anx_ui_session_acme__alpha"]), {
      mode: "hosted",
    });
    expect(probe("acme", "alpha")).toBe(true);
    expect(probe("acme", "beta")).toBe(false);
  });

  it("does not confuse two organizations with the same workspace slug", () => {
    const probe = workspaceSessionProbe(event(["anx_ui_session_acme__alpha"]), {
      mode: "hosted",
    });
    expect(probe("acme", "alpha")).toBe(true);
    expect(probe("other", "alpha")).toBe(false);
  });

  it("does not write or delete cookies", () => {
    // A layout preload stays read-only: the probe reads, nothing else.
    const calls = [];
    const probe = workspaceSessionProbe(
      {
        cookies: {
          get: () => "token",
          set: () => calls.push("set"),
          delete: () => calls.push("delete"),
        },
      },
      { mode: "hosted" },
    );
    probe("acme", "alpha");
    expect(calls).toEqual([]);
  });
});
