import path from "node:path";
import { readFile } from "node:fs/promises";
import { afterEach, expect, it, vi } from "vitest";
// Load the Vite/SvelteKit graph before timed cases; their mocked readiness
// barrier should not include cold toolchain module loading.
import configure, { rendererWarmupPlugin } from "../../vite.config.js";

function warmupServer(bundle) {
  let request;
  let listening;
  const server = {
    middlewares: {
      use: (handler) => {
        request = handler;
      },
    },
    httpServer: {
      once: (_event, handler) => {
        listening = handler;
      },
    },
    transformRequest: vi.fn((url) =>
      url.includes("/deps/")
        ? bundle()
        : Promise.resolve({
            code: 'import dep from "/cache/deps/renderer.js?v=hash";',
          }),
    ),
    waitForRequestsIdle: vi.fn(async () => {}),
    close: vi.fn(async () => {}),
    config: { logger: { error: vi.fn() } },
  };
  return {
    server,
    request: (...args) => request(...args),
    listening: () => listening(),
  };
}

afterEach(() => {
  vi.unstubAllEnvs();
});

it("holds report SSR until optimized dependencies finish loading", async () => {
  let release;
  let requested;
  const loading = new Promise((resolve) => {
    requested = resolve;
  });
  const pending = new Promise((resolve) => {
    release = resolve;
  });
  const fixture = warmupServer(() => {
    requested();
    return pending;
  });
  rendererWarmupPlugin().configureServer(fixture.server);
  const next = vi.fn();
  fixture.request({ url: "/internal/report-preview" }, {}, next);
  const startup = fixture.listening();
  await loading;
  expect(next).not.toHaveBeenCalled();
  release({ code: "export default {};" });
  await startup;
  expect(next).toHaveBeenCalledTimes(1);
  expect(fixture.server.waitForRequestsIdle).toHaveBeenCalledTimes(1);
});

it("closes the renderer on optimization failure without admitting report SSR", async () => {
  const fixture = warmupServer(async () => {
    throw new Error("broken dependency");
  });
  rendererWarmupPlugin().configureServer(fixture.server);
  const next = vi.fn();
  const response = { end: vi.fn() };
  fixture.request({ url: "/internal/report-preview" }, response, next);
  await fixture.listening();
  expect(next).not.toHaveBeenCalled();
  expect(response.statusCode).toBe(503);
  expect(fixture.server.close).toHaveBeenCalledTimes(1);
  expect(fixture.server.config.logger.error).toHaveBeenCalledWith(
    expect.stringContaining("broken dependency"),
  );
});

it("keeps renderer generated modules and dependencies outside the suite's directories", async () => {
  vi.stubEnv("ANX_REPORT_PREVIEW_WORK_DIR", "");
  const suite = configure();
  const directory = path.resolve("test-results", "renderer-instance");
  vi.stubEnv("ANX_REPORT_PREVIEW_WORK_DIR", directory);
  const renderer = configure();
  const { default: kit } = await import("../../svelte.config.js");
  expect(renderer.cacheDir).toBe(path.join(directory, "vite"));
  expect(renderer.cacheDir).not.toBe(suite.cacheDir);
  expect(kit.kit.outDir).toBe(path.join(directory, "svelte-kit"));
  expect(renderer.server.fs.allow).toContain(directory);
});

it("pre-optimizes lazy chart and markdown dependencies without mid-page discovery", async () => {
  vi.stubEnv(
    "ANX_REPORT_PREVIEW_WORK_DIR",
    path.resolve("test-results", "renderer-instance"),
  );
  const { optimizeDeps } = configure();
  expect(optimizeDeps.noDiscovery).toBe(true);
  const sources = await Promise.all([
    readFile(
      new URL(
        "../../src/lib/components/reports/reportChartRenderer.js",
        import.meta.url,
      ),
      "utf8",
    ),
    readFile(new URL("../../src/lib/markdown.js", import.meta.url), "utf8"),
  ]);
  const dependencies = sources.flatMap((source) =>
    [...source.matchAll(/from "([^".]+)"/g)].map((match) => match[1]),
  );
  expect(dependencies.length).toBeGreaterThan(0);
  for (const dependency of dependencies)
    expect(optimizeDeps.include).toContain(dependency);
});
