import { describe, expect, it } from "vitest";

import {
  buildSignInPath,
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
        organizationSlug: "David Zhang",
        workspaceSlug: "Acme Prod",
        workspaceId: "ws_123",
        returnPath: "/topics",
      }),
    ).toBe(
      "/?organization=david-zhang&workspace=acme-prod&workspace_id=ws_123&return_path=%2Ftopics",
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
