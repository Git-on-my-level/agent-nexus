// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { clearAuthSession } from "../../src/lib/authSession.js";
import {
  setCurrentOrganizationSlug,
  setCurrentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";
import {
  cacheWorkRecord,
  clearWorkCache,
  prefetchWork,
  primeWorkSummary,
  readWorkSnapshot,
  workCacheKeys,
  workCacheScope,
} from "../../src/lib/workCache.js";

/** Put the browser in a workspace, the way the shell does on navigation. */
function inWorkspace(org, workspace) {
  setCurrentOrganizationSlug(org);
  setCurrentWorkspaceSlug(workspace);
}

const row = (overrides = {}) => ({
  id: "0199a1e1-0000-7000-8000-000000000001",
  ref: "card:tune-combat",
  handle: "tune-combat",
  title: "Tune core combat loop",
  phase: "in_progress",
  ...overrides,
});

beforeEach(() => {
  inWorkspace("acme", "local");
});

afterEach(() => {
  clearWorkCache();
  inWorkspace("", "");
  vi.useRealTimers();
});

describe("workCacheKeys", () => {
  it("covers every name the router might use for a card", () => {
    expect(workCacheKeys(row())).toEqual([
      "card:tune-combat",
      "tune-combat",
      "tune-combat",
      "0199a1e1-0000-7000-8000-000000000001",
    ]);
  });
});

describe("primed summaries", () => {
  it("is readable by ref, by handle and by id", () => {
    primeWorkSummary(row());
    for (const key of [
      "card:tune-combat",
      "tune-combat",
      "0199a1e1-0000-7000-8000-000000000001",
    ]) {
      expect(readWorkSnapshot(key)?.work.title).toBe("Tune core combat loop");
    }
  });

  it("says it is a list row, not the card", () => {
    primeWorkSummary(row());
    expect(readWorkSnapshot("card:tune-combat")?.full).toBe(false);
    cacheWorkRecord(row({ title: "Tune core combat loop", summary: "body" }));
    expect(readWorkSnapshot("card:tune-combat")?.full).toBe(true);
  });

  it("never lets a later list row overwrite the card that was read", () => {
    cacheWorkRecord(row({ summary: "the whole body" }));
    primeWorkSummary(row());
    const snapshot = readWorkSnapshot("card:tune-combat");
    expect(snapshot?.full).toBe(true);
    expect(snapshot?.work.summary).toBe("the whole body");
  });

  it("goes stale rather than showing a reader a minute-old card as current", () => {
    vi.useFakeTimers();
    primeWorkSummary(row());
    vi.advanceTimersByTime(29_000);
    expect(readWorkSnapshot("card:tune-combat")).not.toBeNull();
    vi.advanceTimersByTime(2_000);
    expect(readWorkSnapshot("card:tune-combat")).toBeNull();
  });

  it("says nothing for an unknown id, and for no id", () => {
    expect(readWorkSnapshot("card:nothing-here")).toBeNull();
    expect(readWorkSnapshot("")).toBeNull();
  });
});

describe("prefetchWork", () => {
  it("reads the card once however many times the pointer crosses the row", async () => {
    const getWork = vi.fn().mockResolvedValue({ work: row() });
    await Promise.all([
      prefetchWork("card:tune-combat", { getWork }),
      prefetchWork("card:tune-combat", { getWork }),
    ]);
    expect(getWork).toHaveBeenCalledTimes(1);
    expect(readWorkSnapshot("card:tune-combat")?.full).toBe(true);
  });

  it("does not read a card it already has in full", async () => {
    cacheWorkRecord(row());
    const getWork = vi.fn().mockResolvedValue({ work: row() });
    await prefetchWork("card:tune-combat", { getWork });
    expect(getWork).not.toHaveBeenCalled();
  });

  it("upgrades a primed list row with a real read", async () => {
    primeWorkSummary(row());
    const getWork = vi
      .fn()
      .mockResolvedValue({ work: row({ summary: "the whole body" }) });
    await prefetchWork("card:tune-combat", { getWork });
    expect(getWork).toHaveBeenCalledTimes(1);
    expect(readWorkSnapshot("card:tune-combat")?.full).toBe(true);
  });

  it("swallows a failed prefetch: a hover must not raise an error", async () => {
    const getWork = vi.fn().mockRejectedValue(new Error("core is down"));
    await expect(
      prefetchWork("card:tune-combat", { getWork }),
    ).resolves.toBeNull();
    expect(readWorkSnapshot("card:tune-combat")).toBeNull();
    // And the failure does not poison the next attempt.
    getWork.mockResolvedValue({ work: row() });
    await prefetchWork("card:tune-combat", { getWork });
    expect(readWorkSnapshot("card:tune-combat")?.full).toBe(true);
  });

  it("survives a client that answers with nothing at all", async () => {
    const getWork = vi.fn().mockReturnValue(undefined);
    await expect(
      prefetchWork("card:tune-combat", { getWork }),
    ).resolves.toBeNull();
  });
});

describe("workspace scoping", () => {
  /*
   * Card handles are unique within a workspace, not across them. Keyed on the
   * handle alone, a card read in one workspace was painted under another
   * workspace's name while that workspace's own read was still in flight.
   */
  it("does not answer another workspace's question", () => {
    cacheWorkRecord(row({ title: "Local release" }));
    expect(readWorkSnapshot("card:tune-combat")?.work.title).toBe(
      "Local release",
    );

    inWorkspace("acme", "other");
    expect(readWorkSnapshot("card:tune-combat")).toBeNull();

    // And the other workspace's own copy of the same handle is its own.
    cacheWorkRecord(row({ title: "Other release" }));
    expect(readWorkSnapshot("card:tune-combat")?.work.title).toBe(
      "Other release",
    );

    inWorkspace("acme", "local");
    expect(readWorkSnapshot("card:tune-combat")?.work.title).toBe(
      "Local release",
    );
  });

  it("separates workspaces of the same name in different organizations", () => {
    cacheWorkRecord(row({ title: "Acme release" }));
    inWorkspace("globex", "local");
    expect(readWorkSnapshot("card:tune-combat")).toBeNull();
  });

  it("prefetches the same handle once per workspace, not once in total", async () => {
    const getWork = vi.fn().mockResolvedValue({ work: row() });
    await prefetchWork("card:tune-combat", { getWork });
    expect(getWork).toHaveBeenCalledTimes(1);
    // Cached here, so no second read...
    await prefetchWork("card:tune-combat", { getWork });
    expect(getWork).toHaveBeenCalledTimes(1);
    // ...but the other workspace has not been asked at all.
    inWorkspace("acme", "other");
    await prefetchWork("card:tune-combat", { getWork });
    expect(getWork).toHaveBeenCalledTimes(2);
  });

  it("files a prefetch that lands after a workspace switch under neither", async () => {
    let settle;
    const getWork = vi.fn(
      () => new Promise((resolve) => (settle = () => resolve({ work: row() }))),
    );
    const inFlight = prefetchWork("card:tune-combat", { getWork });
    // The read is issued a microtask later; let it start before switching.
    await Promise.resolve();
    expect(getWork).toHaveBeenCalledTimes(1);
    // The reader moves on before the answer arrives.
    inWorkspace("acme", "other");
    settle();
    await inFlight;
    expect(readWorkSnapshot("card:tune-combat")).toBeNull();
    inWorkspace("acme", "local");
    expect(readWorkSnapshot("card:tune-combat")).toBeNull();
  });

  it("goes with the session, like every other display cache", () => {
    // A card's title and body are as much of the workspace as a page snapshot
    // is; neither should outlive the identity that was allowed to read it.
    cacheWorkRecord(row());
    expect(readWorkSnapshot("card:tune-combat")).not.toBeNull();
    clearAuthSession("local", { organizationSlug: "acme" });
    expect(readWorkSnapshot("card:tune-combat")).toBeNull();
  });

  it("caches nothing at all when no workspace can be resolved", () => {
    inWorkspace("", "");
    expect(workCacheScope()).toBe("");
    cacheWorkRecord(row());
    expect(readWorkSnapshot("card:tune-combat")).toBeNull();
  });
});
