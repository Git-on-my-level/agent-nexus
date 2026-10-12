// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import {
  isStaleChunkError,
  recoverStaleChunk,
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
