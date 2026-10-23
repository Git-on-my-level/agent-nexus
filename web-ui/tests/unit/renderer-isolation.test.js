import path from "node:path";
import { readFile } from "node:fs/promises";
import { afterEach, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

it("keeps renderer generated modules and dependencies outside the suite's directories", async () => {
  vi.stubEnv("ANX_REPORT_PREVIEW_WORK_DIR", "");
  const { default: configure } = await import("../../vite.config.js");
  const suite = configure();
  const directory = path.resolve("test-results", "renderer-instance");
  vi.stubEnv("ANX_REPORT_PREVIEW_WORK_DIR", directory);
  const renderer = configure();
  vi.resetModules();
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
  const { default: configure } = await import("../../vite.config.js");
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
