import { afterEach, expect, it, vi } from "vitest";

const loadSources = vi.hoisted(() =>
  vi.fn(async () =>
    Array.from({ length: 6 }, () => ({
      status: "fulfilled",
      value: { items: [], work: [] },
    })),
  ),
);
vi.mock("../../src/lib/inboxSources.js", () => ({
  loadInboxSources: loadSources,
}));
vi.mock("../../src/lib/liveWorkspaceEvents.js", () => ({
  liveWorkspaceEvents: () => () => {},
  liveInboxChanges: () => () => {},
}));

import {
  claimInboxCount,
  inboxNeedsYouCount,
  resetInboxCount,
  startInboxCount,
} from "../../src/lib/inboxCount.js";
import { get } from "svelte/store";

afterEach(() => {
  resetInboxCount();
  loadSources.mockClear();
  vi.useRealTimers();
});

it("lets the Inbox page claim the badge before background source reads start", async () => {
  vi.useFakeTimers();
  const stop = startInboxCount("latency-test");
  const release = claimInboxCount();
  await vi.advanceTimersByTimeAsync(1500);
  expect(loadSources).not.toHaveBeenCalled();
  release();
  await vi.advanceTimersByTimeAsync(1500);
  expect(loadSources).toHaveBeenCalledTimes(1);
  stop();
});

it("keeps a new workspace badge unknown until its deferred read completes", async () => {
  vi.useFakeTimers();
  const stop = startInboxCount("other-latency-test");
  expect(get(inboxNeedsYouCount)).toEqual({
    workspace: "other-latency-test",
    count: null,
    truncated: false,
  });
  expect(loadSources).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(1500);
  expect(loadSources).toHaveBeenCalledTimes(1);
  expect(get(inboxNeedsYouCount).count).toBe(0);
  stop();
});
