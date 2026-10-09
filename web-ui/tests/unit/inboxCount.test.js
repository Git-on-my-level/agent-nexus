// @vitest-environment jsdom
import {
  clearWorkspaceViews,
  readWorkspaceView,
  writeWorkspaceView,
} from "../../src/lib/workspaceViewCache.js";
import { readerScopeKey } from "../../src/lib/readerScope.js";
import { commitInboxView } from "../../src/lib/inboxViewCache.js";
import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The sidebar's Needs you badge, when the Inbox page is not the one computing
 * it. Two things are under test, and they want the source loader stubbed in
 * opposite directions:
 *
 * - **When it reads.** The badge's background reads must not race the route
 *   that is painting, and must not run at all while the Inbox page owns the
 *   count. Those tests only care whether `loadInboxSources` was called, so it
 *   is a bare mock returning nothing.
 * - **What it counts.** A blocked card with an answered ask belongs in neither
 *   the page nor the badge, and the only thing that says the ask was answered
 *   is the completed item carrying `responded_at`. Those tests need the real
 *   loader — its index order is the thing that was got wrong — so
 *   `readThrough()` points the mock at the real implementation with a stub
 *   client underneath it.
 */

/** The core client the real loader reads through, for the counting tests. */
const client = vi.hoisted(() => ({
  listPmDecisions: vi.fn(),
  listPmActions: vi.fn(),
  listWork: vi.fn(),
  listInboxItems: vi.fn(),
  getHomeUnread: vi.fn(),
}));
vi.mock("$lib/coreClient", () => ({ coreClient: client }));

const loadSources = vi.hoisted(() => vi.fn());
vi.mock("$lib/inboxSources.js", async (importOriginal) => ({
  // `mergeInboxItems` stays real: it is part of what `inboxCount` does now.
  ...(await importOriginal()),
  loadInboxSources: loadSources,
}));

/*
 * The count subscribes to both event streams. Neither is driven here, but both
 * have to exist: a missing `liveInboxChanges` throws inside the first
 * `startInboxCount`, before any assertion gets a chance to run.
 */
vi.mock("$lib/liveWorkspaceEvents.js", () => ({
  liveWorkspaceEvents: () => () => {},
  liveInboxChanges: () => () => {},
}));

const {
  claimInboxCount,
  inboxNeedsYouCount,
  publishInboxCount,
  resetInboxCount,
  startInboxCount,
} = await import("../../src/lib/inboxCount.js");

/** The background read's delay, as `inboxCount` sets it. */
const REFRESH_DELAY_MS = 1_500;

/** Six settled reads carrying nothing: decisions, actions, work, items x2, unread. */
const emptySources = () =>
  Array.from({ length: 6 }, () => ({
    status: "fulfilled",
    complete: true,
    value: { items: [], work: [] },
  }));

/** Point the mock at the real loader, reading through the stub client. */
async function readThrough() {
  const actual = await vi.importActual("$lib/inboxSources.js");
  loadSources.mockImplementation((options) =>
    actual.loadInboxSources({ ...options, client }),
  );
}

beforeEach(() => {
  clearWorkspaceViews();
  vi.clearAllMocks();
  vi.useFakeTimers();
  loadSources.mockImplementation(async () => emptySources());
  resetInboxCount();
});

afterEach(() => {
  clearWorkspaceViews();
  resetInboxCount();
  vi.useRealTimers();
});

describe("when the badge reads", () => {
  it("lets the Inbox page claim the badge before background source reads start", async () => {
    const stop = startInboxCount("latency-test");
    const release = claimInboxCount();
    await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
    expect(loadSources).not.toHaveBeenCalled();
    release();
    await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
    expect(loadSources).toHaveBeenCalledTimes(1);
    stop();
  });

  it("keeps a new workspace badge unknown until its deferred read completes", async () => {
    const stop = startInboxCount("other-latency-test");
    expect(get(inboxNeedsYouCount)).toEqual({
      workspace: "other-latency-test",
      count: null,
      truncated: false,
    });
    expect(loadSources).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
    expect(loadSources).toHaveBeenCalledTimes(1);
    expect(get(inboxNeedsYouCount).count).toBe(0);
    stop();
  });
});

const NOW = Date.parse("2026-10-07T12:00:00Z");
const at = (hours) => new Date(NOW - hours * 3_600_000).toISOString();

/** A blocked card: in Needs you unless an ask already covers it. */
const BLOCKED_CARD = {
  ref: "card:rollback-wording",
  handle: "rollback-wording",
  id: "0199a1e1-0000-7000-8000-000000000001",
  title: "Approve the rollback wording",
  phase: "blocked",
  owner: "actor:claude",
  source: { authority: "nexus" },
  // Nothing has changed on the card since the ask was answered.
  updated_at: at(6),
};

/** The ask on that card, already answered. */
const ANSWERED_ASK = {
  id: "inbox:ask:thread-1:evt-ask:evt-ask",
  kind: "ask",
  title: "Approve the rollback wording",
  status: "completed",
  subject_ref: BLOCKED_CARD.ref,
  related_refs: [BLOCKED_CARD.ref],
  source_event_time: at(8),
  created_at: at(8),
  responded_at: at(4),
  requester_actor_id: "actor-claude",
};

const OPEN_ASK = {
  ...ANSWERED_ASK,
  id: "inbox:ask:thread-2:evt-open:evt-open",
  status: "open",
  responded_at: undefined,
};

it("a confirmed answer updates the badge and cannot be overwritten by an earlier sidebar read", async () => {
  const key = `${readerScopeKey()}:inbox`;
  const known = emptySources();
  known[3].value.items = [OPEN_ASK];
  writeWorkspaceView(key, known);
  let resolve;
  loadSources.mockReturnValue(
    new Promise((done) => {
      resolve = done;
    }),
  );
  const stop = startInboxCount("local");
  expect(get(inboxNeedsYouCount).count).toBe(1);
  await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
  commitInboxView(readerScopeKey(), {
    answered: { ...OPEN_ASK, status: "completed", responded_at: at(0) },
  });
  expect(get(inboxNeedsYouCount).count).toBe(0);
  resolve(known);
  await vi.advanceTimersByTimeAsync(0);
  expect(get(inboxNeedsYouCount).count).toBe(0);
  expect(readWorkspaceView(key)[3].value.items).toEqual([]);
  stop();
});

/**
 * @param {{ open?: object[], completed?: object[], work?: object[],
 *   failCompleted?: boolean }} sources
 */
async function installSources({
  open = [],
  completed = [],
  work = [BLOCKED_CARD],
  failCompleted = false,
} = {}) {
  client.listPmDecisions.mockResolvedValue({ items: [] });
  client.listPmActions.mockResolvedValue({ items: [] });
  client.listWork.mockResolvedValue({ work, archived_refs: [] });
  client.getHomeUnread.mockResolvedValue({ groups: [] });
  client.listInboxItems.mockImplementation(async ({ status }) => {
    if (status === "completed") {
      if (failCompleted) throw new Error("core unavailable");
      return { items: completed };
    }
    return { items: open };
  });
  await readThrough();
}

/** Start the badge and let its deferred read land. */
async function countFor(workspace = "local") {
  const release = startInboxCount(workspace);
  await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
  const value = get(inboxNeedsYouCount);
  release();
  return value;
}

describe("what the badge counts", () => {
  it("counts a blocked card whose ask is still open only once", async () => {
    // The ask represents the card; the card itself does not add a second row.
    await installSources({ open: [OPEN_ASK] });
    expect((await countFor()).count).toBe(1);
  });

  it("does not bring back a blocked card whose ask was answered", async () => {
    await installSources({ completed: [ANSWERED_ASK] });
    // The regression: with completed items dropped, nothing told the row
    // builder the ask had been answered, so the card came back as 1.
    expect((await countFor()).count).toBe(0);
  });

  it("brings the card back when it moved after the answer", async () => {
    // An answer releases the human; a change since the answer is new work.
    await installSources({
      completed: [ANSWERED_ASK],
      work: [{ ...BLOCKED_CARD, updated_at: at(1) }],
    });
    expect((await countFor()).count).toBe(1);
  });

  it("reads completed items, not just open ones", async () => {
    await installSources({ completed: [ANSWERED_ASK] });
    await countFor();
    const statuses = client.listInboxItems.mock.calls.map(
      ([options]) => options.status,
    );
    expect(statuses).toContain("open");
    expect(statuses).toContain("completed");
  });

  it("keeps the last number when the completed read fails", async () => {
    /*
     * Completed items are load-bearing now, so losing them inflates the count
     * rather than shrinking it. Publishing that would put a badge back on work
     * the reader had already answered — the same bug, from a worse cause. The
     * loader holds the last number instead.
     */
    await installSources({ completed: [ANSWERED_ASK], failCompleted: true });
    publishInboxCount("local", 7);
    const release = startInboxCount("local");
    await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
    // Not 1: the card must not come back because a read failed.
    expect(get(inboxNeedsYouCount).count).toBe(7);
    release();
  });
});

it("keeps unknown receipts in Needs you when a later action page never resolves", async () => {
  await installSources({ work: [] });
  client.listPmDecisions.mockResolvedValue({
    items: [{ id: "unknown", status: "answered", action_id: "missing" }],
  });
  client.listPmActions.mockImplementation(({ cursor }) =>
    cursor
      ? new Promise(() => {})
      : Promise.resolve({ items: [], next_cursor: "slow" }),
  );
  const stop = startInboxCount("local");
  await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS + 45_000);
  expect(get(inboxNeedsYouCount)).toMatchObject({ count: 1, truncated: true });
  stop();
});

it("drops the prior principal's badge immediately in the same workspace", async () => {
  const { authenticatedAgent } = await import("../../src/lib/authSession.js");
  authenticatedAgent.set({ agent_id: "first", actor_id: "first" });
  publishInboxCount("local", 7);
  authenticatedAgent.set({ agent_id: "second", actor_id: "second" });
  expect(get(inboxNeedsYouCount).count).toBeNull();
  authenticatedAgent.set(null);
});

it("badge refresh preserves Updates and never persists a failed later page", async () => {
  const key = `${readerScopeKey()}:inbox`;
  const known = emptySources();
  known[5].value = { groups: [{ group_ref: "thread:updates" }] };
  writeWorkspaceView(key, known);
  const stop = startInboxCount("local");
  await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
  expect(readWorkspaceView(key)[5]).toEqual(known[5]);
  const partial = emptySources();
  partial[0] = {
    status: "fulfilled",
    complete: false,
    value: { items: [{ id: "partial" }] },
    reason: Object.assign(new Error("unavailable"), { status: 503 }),
  };
  loadSources.mockResolvedValue(partial);
  stop();
  const stopAgain = startInboxCount("local");
  await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
  expect(readWorkspaceView(key)[0].value.items).toEqual([]);
  expect(readWorkspaceView(key)[5]).toEqual(known[5]);
  stopAgain();
});

it("an old badge cleanup cannot stop the new principal's count controller", async () => {
  const { authenticatedAgent } = await import("../../src/lib/authSession.js");
  authenticatedAgent.set({ agent_id: "first", actor_id: "first" });
  const oldStop = startInboxCount("local");
  authenticatedAgent.set({ agent_id: "second", actor_id: "second" });
  const newStop = startInboxCount("local");
  oldStop();
  await vi.advanceTimersByTimeAsync(REFRESH_DELAY_MS);
  expect(loadSources).toHaveBeenCalledTimes(1);
  newStop();
  authenticatedAgent.set(null);
});
