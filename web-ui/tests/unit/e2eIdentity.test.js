import { describe, expect, it } from "vitest";

import {
  E2E_IDENTITY_PATH,
  WEB_UI_ROOT,
  identityMismatch,
} from "../../e2eIdentity.js";

const target = {
  role: "web-ui",
  port: 4173,
  variable: "PLAYWRIGHT_PORT",
  basePath: "",
  expected: "/checkouts/mine/web-ui",
};

describe("identityMismatch", () => {
  it("accepts a server started from this checkout", () => {
    expect(
      identityMismatch({
        ...target,
        actual: { root: target.expected, pid: 42, basePath: "" },
      }),
    ).toBeNull();
  });

  it("names the other checkout when a reused server is foreign", () => {
    const message = identityMismatch({
      ...target,
      actual: { root: "/checkouts/theirs/web-ui", pid: 99, basePath: "" },
    });
    expect(message).toContain("/checkouts/theirs/web-ui");
    expect(message).toContain("pid 99");
    expect(message).toContain("port 4173");
    expect(message).toContain("PLAYWRIGHT_REUSE_EXISTING_WEB_UI");
    expect(message).toContain("PLAYWRIGHT_PORT");
  });

  it("explains an unidentifiable server rather than letting specs guess", () => {
    const message = identityMismatch({ ...target, actual: null });
    expect(message).toContain(E2E_IDENTITY_PATH);
    expect(message).toContain(target.expected);
  });

  it("catches the plain and base-path servers being swapped", () => {
    const message = identityMismatch({
      ...target,
      role: "base-path web-ui",
      port: 4176,
      variable: "PLAYWRIGHT_BASE_PATH_PORT",
      basePath: "/anx",
      actual: { root: target.expected, pid: 42, basePath: "" },
    });
    expect(message).toContain("base path");
    expect(message).toContain("/anx");
    expect(message).toContain("PLAYWRIGHT_BASE_PATH_PORT");
  });

  it("treats a missing basePath field as the app root", () => {
    expect(
      identityMismatch({
        ...target,
        actual: { root: target.expected, pid: 42 },
      }),
    ).toBeNull();
  });

  it("resolves this checkout's web-ui directory", () => {
    expect(WEB_UI_ROOT.endsWith("/web-ui")).toBe(true);
  });
});
