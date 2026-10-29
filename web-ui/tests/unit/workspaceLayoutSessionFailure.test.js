/**
 * A workspace session the layout could not read is not an error page. The
 * viewer's navigation renders the shell and its skeleton, and the selection
 * POST establishes the session or reports the failure with its own Retry.
 * Outages defer the read for navigation and route-data loads alike; only a
 * missing workspace (404) renders the definitive "not found" page.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { __resetDev, __setDev } from "$app/environment";

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

const logMocks = vi.hoisted(() => ({ logServerEvent: vi.fn() }));

// A configured hosted core, so the layout runs its schema check.
vi.mock("$lib/server/hostedWorkspaceCore.js", async (importOriginal) => ({
  ...(await importOriginal()),
  hostedWorkspaceCoreBaseUrl: ({ organizationSlug, workspaceSlug }) =>
    `https://cp.example.test/ws/${organizationSlug}/${workspaceSlug}`,
}));

vi.mock("$lib/server/devLog", () => ({
  logServerEvent: logMocks.logServerEvent,
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

afterEach(() => __resetDev());

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

    expect(result.workspaceSession).toEqual({
      agent: null,
      sessionReadDeferred: true,
    });
    expect(result.workspace.slug).toBe("alpha");
  });

  it("reports and logs a core outage as a deferred read, not as signed out", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent.mockRejectedValue(
      coreFailure(503),
    );

    const result = await load(hostedEvent());

    expect(result.workspaceSession).toEqual({
      agent: null,
      sessionReadDeferred: true,
    });
    expect(logMocks.logServerEvent).toHaveBeenCalledWith(
      "workspace.layout.session_read_deferred",
      expect.objectContaining({ org: "acme", slug: "alpha", status: 503 }),
      { level: "warn" },
    );
  });

  it("defers, rather than fails, a route-data load while the runtime is unavailable", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent.mockRejectedValue(
      coreFailure(503),
    );

    const result = await load(hostedEvent({ isDataRequest: true }));

    expect(result.workspaceSession.sessionReadDeferred).toBe(true);
    expect(logMocks.logServerEvent).toHaveBeenCalledWith(
      "workspace.layout.session_read_deferred",
      expect.objectContaining({ status: 503, data_request: true }),
      { level: "warn" },
    );
  });

  it("does not mark a signed-out read as deferred", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);

    const result = await load(hostedEvent());

    expect(result.workspaceSession).toEqual({ agent: null });
    expect(logMocks.logServerEvent).not.toHaveBeenCalledWith(
      "workspace.layout.session_read_deferred",
      expect.anything(),
      expect.anything(),
    );
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

  it("defers a route-data load on any outage, not only a 503", async () => {
    authMocks.loadWorkspaceAuthenticatedAgent.mockRejectedValue(
      coreFailure(502),
    );

    const result = await load(hostedEvent({ isDataRequest: true }));

    expect(result.workspaceSession).toEqual({
      agent: null,
      sessionReadDeferred: true,
    });
  });

  it.each([false, true])(
    "shows the definitive not-found page when the workspace is gone (data request: %s)",
    async (isDataRequest) => {
      authMocks.loadWorkspaceAuthenticatedAgent.mockRejectedValue(
        coreFailure(404),
      );

      await expect(load(hostedEvent({ isDataRequest }))).rejects.toMatchObject({
        status: 404,
        body: { code: "workspace_not_found" },
      });
      expect(logMocks.logServerEvent).toHaveBeenCalledWith(
        "workspace.layout.session_read_not_found",
        expect.objectContaining({ org: "acme", slug: "alpha" }),
        { level: "warn" },
      );
    },
  );

  let schemaCase = 0;
  /** The schema check is memoised per workspace: give each case its own. */
  function freshSchemaWorkspace() {
    const slug = `schema-${++schemaCase}`;
    resolverMocks.resolveWorkspaceInRoute.mockResolvedValue({
      error: null,
      outOfWorkspaceUnauthenticated: false,
      workspace: {
        organizationSlug: "acme",
        slug,
        label: "Schema",
        description: "",
        workspaceId: `ws-${slug}`,
        coreBaseUrl: `https://cp.example.test/ws/acme/${slug}`,
      },
    });
    return slug;
  }

  function schemaCheckFailure(status) {
    // verifyCoreSchemaVersion wraps a transport failure this way.
    const cause = Object.assign(
      new Error("upstream"),
      status ? { status } : {},
    );
    const wrapped = new Error(
      `Unable to verify anx-core schema version: ${cause.message}`,
    );
    wrapped.cause = cause;
    if (status) wrapped.coreHttpStatus = status;
    return wrapped;
  }

  it("shows the not-found page when the schema check finds no workspace (404)", async () => {
    freshSchemaWorkspace();
    authMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);
    coreMocks.verifyCoreSchemaVersion.mockRejectedValue(
      schemaCheckFailure(404),
    );

    await expect(load(hostedEvent())).rejects.toMatchObject({
      status: 404,
      body: { code: "workspace_not_found" },
    });
  });

  it.each([
    ["a 502", 502],
    ["a 504", 504],
    ["a network error", 0],
  ])(
    "defers, rather than fails, when the schema check hits %s",
    async (_label, status) => {
      // Production: dev keeps its own warning banner for these.
      __setDev(false);
      const slug = freshSchemaWorkspace();
      authMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);
      coreMocks.verifyCoreSchemaVersion.mockRejectedValue(
        schemaCheckFailure(status),
      );

      const result = await load(hostedEvent({ isDataRequest: true }));

      expect(result.workspace.slug).toBe(slug);
      expect(result.coreSchemaCheckWarning).toBeUndefined();
      expect(logMocks.logServerEvent).toHaveBeenCalledWith(
        "workspace.layout.schema_check_deferred",
        expect.objectContaining({ org: "acme", slug, status }),
        { level: "warn" },
      );
    },
  );

  it("still fails on a schema mismatch, which is not an outage", async () => {
    freshSchemaWorkspace();
    authMocks.loadWorkspaceAuthenticatedAgent.mockResolvedValue(null);
    coreMocks.verifyCoreSchemaVersion.mockRejectedValue(
      new Error("anx-core schema mismatch: expected 9, received 8."),
    );

    await expect(load(hostedEvent())).rejects.toThrow("schema mismatch");
  });
});
