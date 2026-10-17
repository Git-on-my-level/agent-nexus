// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  isStaleChunkError,
  recoverStaleChunk,
  warnOverviewRecovery,
} from "../../src/lib/chunkRecovery.js";
beforeEach(() => {
  sessionStorage.clear();
  vi.resetModules();
});

afterEach(() => vi.restoreAllMocks());

const chunkError = new TypeError(
  "Failed to fetch dynamically imported module: /_app/old.js",
);

it("does not recover ordinary errors", () => {
  const browser = { sessionStorage, location: { reload: vi.fn() } };
  expect(isStaleChunkError(chunkError)).toBe(true);
  expect(recoverStaleChunk(new Error("network disconnected"), browser)).toBe(
    false,
  );
  expect(browser.location.reload).not.toHaveBeenCalled();
});

it("limits a broken build across events and bootstraps but recovers later deploys", async () => {
  const now = vi.spyOn(Date, "now").mockReturnValue(100_000);
  const browser = { sessionStorage, location: { reload: vi.fn() } };
  const first = await import("../../src/lib/chunkRecovery.js");
  expect(first.recoverStaleChunk(chunkError, browser, "build-a")).toBe(true);
  expect(first.recoverStaleChunk(chunkError, browser, "build-a")).toBe(false);
  vi.resetModules();
  const brokenBootstrap = await import("../../src/lib/chunkRecovery.js");
  expect(
    brokenBootstrap.recoverStaleChunk(chunkError, browser, "build-a"),
  ).toBe(false);
  vi.resetModules();
  const nextBuild = await import("../../src/lib/chunkRecovery.js");
  expect(nextBuild.recoverStaleChunk(chunkError, browser, "build-b")).toBe(
    true,
  );
  vi.resetModules();
  now.mockReturnValue(160_001);
  const thirdBuild = await import("../../src/lib/chunkRecovery.js");
  expect(thirdBuild.recoverStaleChunk(chunkError, browser, "build-c")).toBe(
    true,
  );
  expect(browser.location.reload).toHaveBeenCalledTimes(3);
});

it("stops alternating broken builds across reloads and allows later deployments", async () => {
  const { get } = await import("svelte/store");
  const now = vi.spyOn(Date, "now").mockReturnValue(100_000);
  const browser = { sessionStorage, location: { reload: vi.fn() } };
  for (const [build, expected] of [
    ["a", true],
    ["b", true],
    ["a", false],
    ["b", false],
  ]) {
    vi.resetModules();
    const recovery = await import("../../src/lib/chunkRecovery.js");
    expect(recovery.recoverStaleChunk(chunkError, browser, build)).toBe(
      expected,
    );
    if (!expected) expect(get(recovery.staleChunkRecoveryNeeded)).toBe(true);
  }
  expect(browser.location.reload).toHaveBeenCalledTimes(2);
  now.mockReturnValue(160_001);
  vi.resetModules();
  const later = await import("../../src/lib/chunkRecovery.js");
  expect(later.recoverStaleChunk(chunkError, browser, "c")).toBe(true);
});

it("ignores the legacy lifetime marker", async () => {
  sessionStorage.setItem("anx.chunk-recovery.v1", "1");
  const { recoverStaleChunk } = await import("../../src/lib/chunkRecovery.js");
  const browser = { sessionStorage, location: { reload: vi.fn() } };
  expect(recoverStaleChunk(chunkError, browser)).toBe(true);
});

it.each(["getItem", "setItem"])(
  "requests visible retry when storage %s fails",
  async (method) => {
    const { get } = await import("svelte/store");
    const { recoverStaleChunk, staleChunkRecoveryNeeded } =
      await import("../../src/lib/chunkRecovery.js");
    const browser = {
      sessionStorage: {
        getItem: () => null,
        setItem: () => {},
        [method]: () => {
          throw new Error("Storage denied");
        },
      },
      location: { reload: vi.fn() },
    };
    expect(recoverStaleChunk(chunkError, browser)).toBe(false);
    expect(get(staleChunkRecoveryNeeded)).toBe(true);
    expect(browser.location.reload).not.toHaveBeenCalled();
  },
);

it("recovery diagnostics contain only category, age and numeric status", () => {
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  const now = vi.spyOn(Date, "now").mockReturnValue(50_000);
  warnOverviewRecovery("render-boundary", 10_000, {
    message: "private workspace token",
    status: "private token",
  });
  expect(warn).toHaveBeenCalledTimes(1);
  expect(warn).toHaveBeenCalledWith("anx.overview.recovered", {
    kind: "render-boundary",
    ageMs: 40_000,
    status: null,
  });
  warnOverviewRecovery("refresh-return", null, { coreHttpStatus: 401 });
  expect(warn).toHaveBeenLastCalledWith("anx.overview.recovered", {
    kind: "refresh-return",
    ageMs: null,
    status: 401,
  });
  now.mockRestore();
  warn.mockRestore();
});
