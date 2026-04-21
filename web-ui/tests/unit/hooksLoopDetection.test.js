import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Hoisted mocks so the module under test sees them when imported below.
const coreRouteCatalogMocks = vi.hoisted(() => ({
  isProxyableCommand: vi.fn(() => false),
}));

const proxyWorkspaceTargetMocks = vi.hoisted(() => ({
  resolveProxyTarget: vi.fn(() =>
    Promise.resolve({ coreBaseUrl: "", workspace: { slug: "ops" } }),
  ),
}));

vi.mock("$app/environment", () => ({
  dev: true,
}));

vi.mock("$env/dynamic/private", () => ({
  env: {},
}));

vi.mock("$lib/coreRouteCatalog", () => ({
  isProxyableCommand: coreRouteCatalogMocks.isProxyableCommand,
}));

vi.mock("$lib/compat/workspaceCompat", () => ({
  getWorkspaceHeader: vi.fn(() => "ops"),
}));

vi.mock("$lib/workspacePaths", async (importOriginal) => {
  const mod = await importOriginal();
  return {
    ...mod,
    stripBasePath: vi.fn((pathname) => pathname),
  };
});

vi.mock("$lib/server/authSession", () => ({
  clearWorkspaceAuthSession: vi.fn(),
  getWorkspaceAuthSession: vi.fn(() => ({ accessToken: "" })),
  isRetryableWorkspaceRefreshFailure: vi.fn(() => false),
  readWorkspaceRefreshToken: vi.fn(() => ""),
  refreshWorkspaceAuthSession: vi.fn(async () => {}),
  shouldClearWorkspaceAuthSessionAfterRetryableFailure: vi.fn(() => false),
}));

vi.mock("$lib/server/proxyWorkspaceTarget", () => ({
  resolveProxyTarget: proxyWorkspaceTargetMocks.resolveProxyTarget,
}));

vi.mock("$lib/server/directCoreProxyPaths", () => ({
  isDirectCoreProxyPath: vi.fn(() => false),
}));

const { handle, __resetLoopTrackerForTests } =
  await import("../../src/hooks.server.js");

function makeNavRequest(pathname) {
  return {
    url: new URL(`https://oar.example.test${pathname}`),
    request: new Request(`https://oar.example.test${pathname}`, {
      method: "GET",
      headers: {
        accept: "application/json",
      },
    }),
    route: { id: "/o/[organization]/w/[workspace]/login" },
  };
}

function makeDataFetch(pathname) {
  return {
    url: new URL(`https://oar.example.test${pathname}/__data.json`),
    request: new Request(`https://oar.example.test${pathname}/__data.json`, {
      method: "GET",
      headers: {
        accept: "application/json",
      },
    }),
    route: { id: "/o/[organization]/w/[workspace]/login" },
  };
}

describe("hooks.server loop detection (dev mode)", () => {
  /** @type {ReturnType<typeof vi.spyOn>} */
  let consoleLog;
  /** @type {ReturnType<typeof vi.spyOn>} */
  let consoleWarn;

  beforeEach(() => {
    __resetLoopTrackerForTests();
    consoleLog = vi.spyOn(console, "log").mockImplementation(() => {});
    consoleWarn = vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  afterEach(() => {
    consoleLog.mockRestore();
    consoleWarn.mockRestore();
  });

  it("logs ssr.request lines for SvelteKit __data.json fetches, not just document navigations", async () => {
    const event = makeDataFetch("/o/my-org/w/my-ws/login");
    const resolve = vi.fn(async () => new Response("{}", { status: 200 }));

    await handle({ event, resolve });

    const dataLog = consoleLog.mock.calls
      .map((args) => String(args[0]))
      .find(
        (line) => line.includes("ssr.request") && line.includes('kind="data"'),
      );
    expect(dataLog).toBeDefined();
    expect(dataLog).toMatch(/__data\.json/);
  });

  it("short-circuits with 503 when the same path fires above the threshold within the window", async () => {
    const event = makeDataFetch("/o/my-org/w/my-ws/login");
    const resolve = vi.fn(async () => new Response("{}", { status: 200 }));

    const responses = [];
    for (let i = 0; i < 8; i += 1) {
      responses.push(await handle({ event, resolve }));
    }

    expect(resolve).toHaveBeenCalledTimes(7);
    expect(responses[responses.length - 1]?.status).toBe(503);
    const body = await responses[responses.length - 1].json();
    expect(body?.error?.code).toBe("request_loop_detected");

    const loopLog = consoleWarn.mock.calls
      .map((args) => String(args[0]))
      .find((line) => line.includes("ssr.request.loop_short_circuit"));
    expect(loopLog).toBeDefined();
    expect(loopLog).toMatch(/path="\/o\/my-org\/w\/my-ws\/login\/__data.json"/);
  });

  it("does not flag loop_detected for a normal navigation rate", async () => {
    const resolve = vi.fn(
      async () =>
        new Response("<html></html>", {
          status: 200,
          headers: { "content-type": "text/html" },
        }),
    );

    for (let i = 0; i < 3; i += 1) {
      await handle({
        event: makeNavRequest("/o/my-org/w/my-ws/login"),
        resolve,
      });
    }

    const loopLog = consoleWarn.mock.calls
      .map((args) => String(args[0]))
      .find((line) => line.includes("ssr.request.loop_detected"));
    expect(loopLog).toBeUndefined();
  });
});
