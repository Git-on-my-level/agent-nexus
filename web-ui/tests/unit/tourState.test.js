import { afterEach, describe, expect, it, vi } from "vitest";

function mockLocalStorage() {
  const store = {};
  const api = {
    getItem: vi.fn((key) => store[key] ?? null),
    setItem: vi.fn((key, val) => {
      store[key] = String(val);
    }),
    removeItem: vi.fn((key) => {
      delete store[key];
    }),
    clear: vi.fn(() => {
      for (const k of Object.keys(store)) delete store[k];
    }),
    _store: store,
  };
  return api;
}

describe("tourState", () => {
  let tourState;

  afterEach(() => {
    vi.resetModules();
    vi.unstubAllGlobals();
  });

  async function importWithLocalStorage(localStorageMock) {
    vi.stubGlobal("localStorage", localStorageMock);
    tourState = await import("../../src/lib/tourState.js");
    return tourState;
  }

  it("workspace tour seen key and mark", async () => {
    const lsMock = mockLocalStorage();
    const { isWorkspaceTourSeen, markWorkspaceTourSeen, workspaceTourSeenKey } =
      await importWithLocalStorage(lsMock);
    expect(workspaceTourSeenKey("ws1")).toBe("workspaceTourSeen.ws1");
    expect(isWorkspaceTourSeen("ws1")).toBe(false);
    markWorkspaceTourSeen("ws1");
    expect(isWorkspaceTourSeen("ws1")).toBe(true);
    expect(lsMock._store["workspaceTourSeen.ws1"]).toBe("1");
  });
});
