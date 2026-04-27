/**
 * Exercises +layout.server.js schema verification when the proxied core returns
 * 5xx in dev: SSR should not hard-fail; it returns `coreSchemaCheckWarning` instead.
 */
import { beforeEach, describe, expect, it, vi } from "vitest";

import { mockLocalProvider } from "../fixtures/workspaceAuth.js";

const workspaceResolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
  resolveWorkspaceCatalog: vi.fn(),
}));

const anxCoreClientMocks = vi.hoisted(() => ({
  createAnxCoreClient: vi.fn(() => ({})),
  verifyCoreSchemaVersion: vi.fn(),
}));

vi.mock("$app/environment", async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, dev: true };
});

vi.mock("$lib/anxCoreClient", () => ({
  createAnxCoreClient: anxCoreClientMocks.createAnxCoreClient,
  verifyCoreSchemaVersion: anxCoreClientMocks.verifyCoreSchemaVersion,
}));

vi.mock("$lib/server/workspaceResolver", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceInRoute: workspaceResolverMocks.resolveWorkspaceInRoute,
    resolveWorkspaceCatalog: workspaceResolverMocks.resolveWorkspaceCatalog,
  };
});

import { load } from "../../src/routes/o/[organization]/w/[workspace]/+layout.server.js";

describe("workspace +layout.server.js core schema (dev degradation)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceResolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      workspaces: [],
      defaultWorkspaceSlug: null,
      defaultOrganizationSlug: null,
    });
  });

  it("returns coreSchemaCheckWarning in dev when handshake gets 502 from core", async () => {
    const e = new Error(
      "Unable to verify anx-core schema version: anx-core request failed (502) - workspace",
    );
    e.coreHttpStatus = 502;
    anxCoreClientMocks.verifyCoreSchemaVersion.mockRejectedValue(e);

    workspaceResolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      outOfWorkspaceUnauthenticated: false,
      workspace: {
        organizationSlug: "my-org",
        slug: "my-ws",
        label: "My WS",
        description: "",
        coreBaseUrl: "http://localhost:5173/ws/my-org/my-ws",
      },
    });

    const event = {
      params: { organization: "my-org", workspace: "my-ws" },
      url: new URL("https://ui.example.test/o/my-org/w/my-ws/threads"),
      fetch: vi.fn(),
      cookies: {
        get: vi.fn((name) =>
          name === "anx_ui_session_my-org__my-ws" ? "refresh" : "",
        ),
        set: vi.fn(),
        delete: vi.fn(),
      },
      locals: { outOfWorkspace: mockLocalProvider() },
    };

    const result = await load(event);
    expect(result.coreSchemaCheckWarning).toBeTruthy();
    expect(String(result.coreSchemaCheckWarning)).toContain("502");
  });
});
