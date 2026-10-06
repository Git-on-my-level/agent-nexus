import { describe, expect, it } from "vitest";

import {
  buildSignInPath,
  confineWorkspaceReturnPath,
  readLaunchParams,
  sanitizeReturnPath,
} from "../../src/lib/workspaceLaunchFlow.js";

describe("launchFlow helpers", () => {
  it("sanitizes return paths to app-local absolute paths", () => {
    expect(sanitizeReturnPath("/topics?filter=open")).toBe(
      "/topics?filter=open",
    );
    expect(sanitizeReturnPath("/docs/v1.2/release-notes")).toBe(
      "/docs/v1.2/release-notes",
    );
    expect(sanitizeReturnPath("/a..b/c")).toBe("/a..b/c");
    expect(sanitizeReturnPath("topics")).toBe("/");
    expect(sanitizeReturnPath("//evil.test/path")).toBe("/");
    expect(sanitizeReturnPath("/topics\nmalformed")).toBe("/");
    expect(sanitizeReturnPath("/../x")).toBe("/");
    expect(sanitizeReturnPath("/a/../x")).toBe("/");
    expect(sanitizeReturnPath("/./x")).toBe("/");
    expect(sanitizeReturnPath("/%2e%2e/x")).toBe("/");
    expect(sanitizeReturnPath("/%2E./x")).toBe("/");
    expect(sanitizeReturnPath("/%252e%252e/x")).toBe("/");
  });

  it("rejects anything a browser would renormalize into a separator", () => {
    // Chromium turns backslashes into `/` and strips control characters when it
    // resolves a location, so a value that looks confined to one prefix can walk
    // out of it. These are the forms the control plane's launch validator
    // rejects; the sanitizer must reject them too.
    expect(
      sanitizeReturnPath(
        "/a\\..\\..\\..\\..\\..\\o\\other\\w\\private\\agents",
      ),
    ).toBe("/");
    expect(sanitizeReturnPath("/topics\\..\\..\\x")).toBe("/");
    expect(sanitizeReturnPath("\\\\evil.test/path")).toBe("/");
    expect(sanitizeReturnPath("/\\evil.test/path")).toBe("/");
    expect(sanitizeReturnPath("/top\tics")).toBe("/");
    expect(sanitizeReturnPath("/top\u0000ics")).toBe("/");
    expect(sanitizeReturnPath("/top\u007fics")).toBe("/");
    expect(sanitizeReturnPath("/topics\rmalformed")).toBe("/");
    expect(sanitizeReturnPath("/topics#fragment")).toBe("/");
  });

  it("rejects encoded separators and malformed escapes in the path", () => {
    expect(sanitizeReturnPath("/a%5c..%5c..%5cx")).toBe("/");
    expect(sanitizeReturnPath("/a%5C..%5C..%5Cx")).toBe("/");
    expect(sanitizeReturnPath("/a%2f..%2f..%2fx")).toBe("/");
    expect(sanitizeReturnPath("/a%2F..%2F..%2Fx")).toBe("/");
    expect(sanitizeReturnPath("/topics%zz")).toBe("/");
    expect(sanitizeReturnPath("/topics%2")).toBe("/");
    expect(sanitizeReturnPath("/topics%")).toBe("/");
    // A separator that only spells itself out after a round of decoding is
    // the same risk, however many rounds it takes.
    expect(sanitizeReturnPath("/a%255c..%255cx")).toBe("/");
    expect(sanitizeReturnPath("/a%252f..%252fx")).toBe("/");
    expect(sanitizeReturnPath("/a%25252f..%25252fx")).toBe("/");
    expect(sanitizeReturnPath("/a%252%66..%252%66x")).toBe("/");
    expect(sanitizeReturnPath("/a%25%32%66x")).toBe("/");
    expect(sanitizeReturnPath("/a%2509b")).toBe("/");
  });

  it("leaves the query alone: nothing after ? can become part of the path", () => {
    // An OAuth continuation carries a percent-encoded redirect_uri, and search
    // boxes encode a typed slash. Rejecting those would break sign-in routing
    // and drop viewers at the workspace root instead of where they were.
    const mcp =
      "/hosted/mcp/authorize?client_id=c1&redirect_uri=https%3A%2F%2Fclaude.ai%2Fapi%2Fmcp%2Fauth_callback&response_type=code&state=xyz";
    expect(sanitizeReturnPath(mcp, "")).toBe(mcp);
    expect(sanitizeReturnPath("/docs?q=and%2For")).toBe("/docs?q=and%2For");
    expect(sanitizeReturnPath("/tasks?project_ref=github%2Facme%2Frepo")).toBe(
      "/tasks?project_ref=github%2Facme%2Frepo",
    );
    // ...but a control character still splits a Location header wherever it is.
    expect(sanitizeReturnPath("/docs?q=a\r\nSet-Cookie:+x")).toBe("/");
  });

  it("still accepts ordinary workspace-relative paths", () => {
    expect(sanitizeReturnPath("/agents")).toBe("/agents");
    expect(sanitizeReturnPath("/inbox?tab=needs-you")).toBe(
      "/inbox?tab=needs-you",
    );
    expect(sanitizeReturnPath("/docs/My%20Doc")).toBe("/docs/My%20Doc");
    expect(sanitizeReturnPath("/tasks/work_01HX")).toBe("/tasks/work_01HX");
  });

  it("reads launch continuation params from search params", () => {
    const params = new URLSearchParams(
      "workspace=Acme-Prod&workspace_id=ws_123&return_to=%2Ftopics%3Ftag%3Dhot",
    );
    expect(readLaunchParams(params)).toEqual({
      organizationSlug: "",
      workspaceSlug: "acme-prod",
      workspaceId: "ws_123",
      returnPath: "/topics?tag=hot",
      hasContinuation: true,
    });
  });

  it("builds sign-in path with launch continuation params", () => {
    expect(
      buildSignInPath({
        organizationSlug: "Alex Morgan",
        workspaceSlug: "Acme Prod",
        workspaceId: "ws_123",
        returnPath: "/topics",
      }),
    ).toBe(
      "/?organization=alex-morgan&workspace=acme-prod&workspace_id=ws_123&return_path=%2Ftopics",
    );

    expect(
      buildSignInPath({
        workspaceSlug: "acme-prod",
        workspaceId: "ws_123",
        returnPath: "/",
      }),
    ).toBe("/?workspace=acme-prod&workspace_id=ws_123");
  });
});

describe("confineWorkspaceReturnPath", () => {
  const origin = "https://anx.example.test";
  const confine = (returnPath) =>
    confineWorkspaceReturnPath({
      origin,
      organizationSlug: "acme",
      workspaceSlug: "alpha",
      returnPath,
    });

  it("carries an ordinary return path and its query into the workspace", () => {
    expect(confine("/agents")).toBe("/o/acme/w/alpha/agents");
    expect(confine("/inbox?tab=needs-you")).toBe(
      "/o/acme/w/alpha/inbox?tab=needs-you",
    );
    expect(confine("/")).toBe("/o/acme/w/alpha");
    expect(confine(undefined)).toBe("/o/acme/w/alpha");
  });

  // The sanitizer rejects these too; this is the second, independent layer, so
  // it has to hold on a value that reached it anyway.
  it.each([
    ["backslash traversal", "/a\\..\\..\\..\\o\\other\\w\\private\\agents"],
    ["embedded tab", "/a\t/../../o/other"],
    ["dot segments", "/../../o/other/w/private"],
    ["sibling workspace", "/../alpha-2/agents"],
    ["carriage return", "/a\r\n/../../o/other"],
    ["protocol-relative", "//evil.test/path"],
    ["backslash authority", "/\\evil.test/path"],
    ["absolute url", "https://evil.test/path"],
  ])("keeps %s inside the workspace", (_label, returnPath) => {
    const landed = new URL(confine(returnPath), origin);
    expect(landed.origin).toBe(new URL(origin).origin);
    expect(
      landed.pathname === "/o/acme/w/alpha" ||
        landed.pathname.startsWith("/o/acme/w/alpha/"),
    ).toBe(true);
  });

  it("works without a request origin, and still rejects an absolute target", () => {
    const args = { organizationSlug: "acme", workspaceSlug: "alpha" };
    expect(confineWorkspaceReturnPath({ ...args, returnPath: "/agents" })).toBe(
      "/o/acme/w/alpha/agents",
    );
    expect(
      confineWorkspaceReturnPath({
        ...args,
        returnPath: "https://evil.test/x",
      }),
    ).toBe("/o/acme/w/alpha");
  });
});
