import { afterEach, describe, expect, it, vi } from "vitest";

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

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

  it("shouldShowTour returns true for fresh workspace with zero items", async () => {
    const { shouldShowTour } = await importWithLocalStorage(mockLocalStorage());
    expect(shouldShowTour({ workspaceSlug: "acme", totalItems: 0 })).toBe(true);
  });

  it("shouldShowTour returns false when workspace has items", async () => {
    const { shouldShowTour } = await importWithLocalStorage(mockLocalStorage());
    expect(shouldShowTour({ workspaceSlug: "acme", totalItems: 5 })).toBe(
      false,
    );
  });

  it("shouldShowTour returns false after manual dismiss", async () => {
    const lsMock = mockLocalStorage();
    const { shouldShowTour, dismissTour } =
      await importWithLocalStorage(lsMock);
    dismissTour("acme");
    expect(shouldShowTour({ workspaceSlug: "acme", totalItems: 0 })).toBe(
      false,
    );
  });

  it("shouldShowTour returns false after firstSeen exceeds 7 days", async () => {
    const lsMock = mockLocalStorage();
    const { shouldShowTour } = await importWithLocalStorage(lsMock);

    const oldTimestamp = Date.now() - SEVEN_DAYS_MS - 1000;
    lsMock._store[`anx.tour.inbox.v1.acme.firstSeen`] = String(oldTimestamp);

    expect(shouldShowTour({ workspaceSlug: "acme", totalItems: 0 })).toBe(
      false,
    );
  });

  it("shouldShowTour returns false when workspaceSlug is empty", async () => {
    const { shouldShowTour } = await importWithLocalStorage(mockLocalStorage());
    expect(shouldShowTour({ workspaceSlug: "", totalItems: 0 })).toBe(false);
  });

  it("dismissTour sets localStorage key to dismissed", async () => {
    const lsMock = mockLocalStorage();
    const { dismissTour, isTourDismissed } =
      await importWithLocalStorage(lsMock);
    dismissTour("acme");
    expect(isTourDismissed("acme")).toBe(true);
    expect(lsMock._store["anx.tour.inbox.v1.acme"]).toBe("dismissed");
  });

  it("firstSeen is stamped on first check and preserved on subsequent checks", async () => {
    const lsMock = mockLocalStorage();
    const { shouldShowTour } = await importWithLocalStorage(lsMock);

    shouldShowTour({ workspaceSlug: "acme", totalItems: 0 });
    const stamped = lsMock._store["anx.tour.inbox.v1.acme.firstSeen"];
    expect(stamped).toBeTruthy();

    const before = Number(stamped);
    shouldShowTour({ workspaceSlug: "acme", totalItems: 0 });
    const after = Number(lsMock._store["anx.tour.inbox.v1.acme.firstSeen"]);
    expect(after).toBe(before);
  });

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

  it("inbox kind seen key and mark", async () => {
    const lsMock = mockLocalStorage();
    const { isInboxKindSeen, markInboxKindSeen, inboxKindSeenKey } =
      await importWithLocalStorage(lsMock);
    expect(inboxKindSeenKey("ask")).toBe("inboxKindSeen.ask");
    expect(isInboxKindSeen("ask")).toBe(false);
    markInboxKindSeen("ask");
    expect(isInboxKindSeen("ask")).toBe(true);
  });
});
