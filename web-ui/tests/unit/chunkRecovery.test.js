// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import {
  isStaleChunkError,
  recoverStaleChunk,
  warnOverviewRecovery,
} from "../../src/lib/chunkRecovery.js";
it("only reloads import failures and stops after one attempt across bootstraps", async () => {
  sessionStorage.clear();
  const browser = { sessionStorage, location: { reload: vi.fn() } };
  expect(recoverStaleChunk(new Error("network disconnected"), browser)).toBe(
    false,
  );
  const error = new TypeError(
    "Failed to fetch dynamically imported module: /_app/old.js",
  );
  expect(isStaleChunkError(error)).toBe(true);
  expect(recoverStaleChunk(error, browser)).toBe(true);
  vi.resetModules();
  const fresh = await import("../../src/lib/chunkRecovery.js");
  expect(fresh.recoverStaleChunk(error, browser)).toBe(false);
  expect(browser.location.reload).toHaveBeenCalledTimes(1);
});

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
