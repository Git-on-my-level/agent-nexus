import { describe, expect, it } from "vitest";

import {
  buildHostedSignInPath,
  normalizeHostedLaunchFinishURL,
  readHostedLaunchParams,
  sanitizeHostedReturnPath,
} from "../../src/lib/hosted/launchFlow.js";

describe("launchFlow helpers", () => {
  it("sanitizes return paths to app-local absolute paths", () => {
    expect(sanitizeHostedReturnPath("/topics?filter=open")).toBe(
      "/topics?filter=open",
    );
    expect(sanitizeHostedReturnPath("/docs/v1.2/release-notes")).toBe(
      "/docs/v1.2/release-notes",
    );
    expect(sanitizeHostedReturnPath("/a..b/c")).toBe("/a..b/c");
    expect(sanitizeHostedReturnPath("topics")).toBe("/");
    expect(sanitizeHostedReturnPath("//evil.test/path")).toBe("/");
    expect(sanitizeHostedReturnPath("/topics\nmalformed")).toBe("/");
    expect(sanitizeHostedReturnPath("/../x")).toBe("/");
    expect(sanitizeHostedReturnPath("/a/../x")).toBe("/");
    expect(sanitizeHostedReturnPath("/./x")).toBe("/");
    expect(sanitizeHostedReturnPath("/%2e%2e/x")).toBe("/");
    expect(sanitizeHostedReturnPath("/%2E./x")).toBe("/");
    expect(sanitizeHostedReturnPath("/%252e%252e/x")).toBe("/");
  });

  it("reads launch continuation params from search params", () => {
    const params = new URLSearchParams(
      "workspace=Acme-Prod&workspace_id=ws_123&return_to=%2Fthreads%3Ftag%3Dhot",
    );
    expect(readHostedLaunchParams(params)).toEqual({
      workspaceSlug: "acme-prod",
      workspaceId: "ws_123",
      returnPath: "/threads?tag=hot",
      hasContinuation: true,
    });
  });

  it("builds hosted sign-in path with launch continuation params", () => {
    expect(
      buildHostedSignInPath({
        workspaceSlug: "Acme Prod",
        workspaceId: "ws_123",
        returnPath: "/threads",
      }),
    ).toBe(
      "/hosted/signin?workspace=acme-prod&workspace_id=ws_123&return_path=%2Fthreads",
    );

    expect(
      buildHostedSignInPath({
        workspaceSlug: "acme-prod",
        workspaceId: "ws_123",
        returnPath: "/",
      }),
    ).toBe("/hosted/signin?workspace=acme-prod&workspace_id=ws_123");
  });

  it("normalizes control-plane launch finish urls for browser navigation", () => {
    expect(
      normalizeHostedLaunchFinishURL("/workspaces/ws/launch-finish?lid=1"),
    ).toBe("/hosted/api/workspaces/ws/launch-finish?lid=1");
    expect(
      normalizeHostedLaunchFinishURL(
        "https://control.example.test/workspaces/ws/launch-finish?lid=1",
      ),
    ).toBe("https://control.example.test/workspaces/ws/launch-finish?lid=1");
    expect(
      normalizeHostedLaunchFinishURL(
        "/hosted/api/workspaces/ws/launch-finish?lid=1",
      ),
    ).toBe("/hosted/api/workspaces/ws/launch-finish?lid=1");
  });
});
