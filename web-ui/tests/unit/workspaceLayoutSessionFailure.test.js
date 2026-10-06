/**
 * A workspace session the layout could not read is not an error page. The
 * viewer's navigation renders the shell and its skeleton, and the selection
 * POST establishes the session or reports the failure with its own Retry.
 * Route-data loads keep failing loudly so a broken preload stays visible.
 */
import { beforeEach, describe, expect, it, vi } from "vitest";

import { mockHostedProvider } from "../fixtures/workspaceAuth.js";

const resolverMocks = vi.hoisted(() => ({
  resolveWorkspaceInRoute: vi.fn(),
  resolveWorkspaceCatalog: vi.fn(),
}));

const coreMocks = vi.hoisted(() => ({
  createAnxCoreClient: vi.fn(() => ({})),
  verifyCoreSchemaVersion: vi.fn(async () => {}),
}));

const authMocks = vi.hoisted(() => ({
  loadWorkspaceAuthenticatedAgent: vi.fn(),
}));

vi.mock("$lib/anxCoreClient", () => ({
  createAnxCoreClient: coreMocks.createAnxCoreClient,
  verifyCoreSchemaVersion: coreMocks.verifyCoreSchemaVersion,
}));

vi.mock("$lib/server/authSession.js", () => ({
  loadWorkspaceAuthenticatedAgent: authMocks.loadWorkspaceAuthenticatedAgent,
}));

vi.mock("$lib/server/workspaceResolver", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    resolveWorkspaceInRoute: resolverMocks.resolveWorkspaceInRoute,
    resolveWorkspaceCatalog: resolverMocks.resolveWorkspaceCatalog,
  };
});

import { load } from "../../src/routes/o/[organization]/w/[workspace]/+layout.server.js";

function hostedEvent(overrides = {}) {
  return {
    params: { organization: "acme", workspace: "alpha" },
    url: new URL("https://anx.example.test/o/acme/w/alpha/agents"),
    request: { method: "GET", headers: new Headers() },
    fetch: vi.fn(),
    setHeaders: vi.fn(),
    cookies: { get: vi.fn(() => ""), set: vi.fn(), delete: vi.fn() },
    locals: { outOfWorkspace: mockHostedProvider() },
    ...overrides,
  };
}

function coreFailure(status) {
  const failure = new Error("workspace runtime backend is unavailable");
  failure.status = status;
  return failure;
}

describe("workspace +layout.server.js unreadable session", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    coreMocks.verifyCoreSchemaVersion.mockResolvedValue(undefined);
    resolverMocks.resolveWorkspaceCatalog.mockResolvedValue({
      workspaces: [],
      defaultWorkspaceSlug: null,
      defaultOrganizationSlug: null,
    });
    resolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      outOfWorkspaceUnauthenticated: false,
      workspace: {
        organizationSlug: "acme",
        slug: "alpha",
        label: "Alpha",
        description: "",
        workspaceId: "ws-1",
        // Unique per test file: the schema check is memoised per workspace.
        coreBaseUrl: "https://cp.example.test/ws/acme/alpha",
      },
    });
  });

  it("renders the shell with no session when the navigation cannot read one", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent.mockRejectedValue(
      coreFailure(502),
    );

    const result = await load(hostedEvent());

    expect(result.workspaceSession).toEqual({ agent: null });
    expect(result.workspace.slug).toBe("alpha");
  });

  it("keeps the session read speculative on every load", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    await load(hostedEvent());

    // A page render must not rotate tokens, wake a runtime or record activity.
    expect(authMocks.loadWorkspaceAuthenticatedAgent).toHaveBeenCalledWith(
      expect.objectContaining({
        readOnly: true,
        headers: expect.objectContaining({ purpose: "prefetch" }),
      }),
    );
  });

  it("still fails a route-data load whose session read errors", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent.mockRejectedValue(
      coreFailure(502),
    );

    await expect(
      load(hostedEvent({ isDataRequest: true })),
    ).rejects.toMatchObject({ status: 502 });
  });
});
