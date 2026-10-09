import { describe, expect, it } from "vitest";

import { resolveCliBaseUrl } from "../../src/lib/server/cliBaseUrl.js";

const at = (href) => ({ url: new URL(href) });

describe("base URL for copied anx commands", () => {
  it("prefers the workspace's configured core API origin", () => {
    expect(
      resolveCliBaseUrl(at("https://app.example.test/o/local/w/ops/pm/setup"), {
        organizationSlug: "local",
        workspaceSlug: "ops",
        workspace: {
          coreBaseUrl: "http://127.0.0.1:8002",
          publicOrigin: "https://stale.example.test",
        },
      }),
    ).toBe("http://127.0.0.1:8002");
  });

  /*
   * The copied command is usually run on another machine, so a loopback
   * origin is a last resort rather than a fallback.
   */
  it("falls back to the public workspace URL for a loopback request", () => {
    expect(
      resolveCliBaseUrl(at("http://127.0.0.1:4173/o/local/w/ops/pm/setup"), {
        organizationSlug: "local",
        workspaceSlug: "ops",
        workspace: {
          coreBaseUrl: "",
          publicOrigin: "https://anx.example.test",
        },
      }),
    ).toBe("https://anx.example.test/o/local/w/ops");
  });

  it("uses the request's own workspace URL when it is reachable", () => {
    expect(
      resolveCliBaseUrl(at("https://anx.example.test/o/local/w/ops/tasks"), {
        organizationSlug: "local",
        workspaceSlug: "ops",
        workspace: { coreBaseUrl: "" },
      }),
    ).toBe("https://anx.example.test/o/local/w/ops");
  });

  /*
   * A deployment served under a base path (`ANX_UI_BASE_PATH`, or a prefix a
   * reverse proxy adds that this process never sees) must keep it: a command
   * copied without the prefix hits nothing.
   */
  it("keeps a base-path prefix from any route under the workspace", () => {
    for (const route of ["/anx/o/local/w/ops/access", "/anx/o/local/w/ops"]) {
      expect(
        resolveCliBaseUrl(at(`https://anx.example.test${route}`), {
          organizationSlug: "local",
          workspaceSlug: "ops",
          workspace: { coreBaseUrl: "" },
        }),
      ).toBe("https://anx.example.test/anx/o/local/w/ops");
    }
  });

  /*
   * The URL segment is normalized before resolution, so it can differ in case
   * from the catalog's canonical slug. A case-sensitive marker search would
   * miss it and drop the prefix with it.
   */
  it("finds the workspace root however the URL spelled the slugs", () => {
    for (const [segments, slug] of [
      ["/o/Local/w/OPS", "ops"],
      ["/o/local/w/ops_v2", "ops-v2"],
    ]) {
      expect(
        resolveCliBaseUrl(at(`https://anx.example.test/anx${segments}/pm`), {
          organizationSlug: "local",
          workspaceSlug: slug,
          workspace: { coreBaseUrl: "" },
        }),
      ).toBe(`https://anx.example.test/anx${segments}`);
    }
  });

  it("resolves the slugs from either shape of the resolved workspace", () => {
    expect(
      resolveCliBaseUrl(at("https://anx.example.test/o/local/w/ops/pm/setup"), {
        workspace: {
          organizationSlug: "local",
          slug: "ops",
          coreBaseUrl: "",
        },
      }),
    ).toBe("https://anx.example.test/o/local/w/ops");
  });

  it("returns an empty string rather than a wrong origin when nothing is known", () => {
    expect(resolveCliBaseUrl(at("https://anx.example.test/"), {})).toBe("");
  });
});
