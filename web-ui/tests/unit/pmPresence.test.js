import { afterEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

import { PM_STATES } from "../../src/lib/pm/onboardingState.js";
import {
  SHELL_REFRESH_MIN_MS,
  UNREADABLE_POLL_LIMIT,
  clearPmPresence,
  fetchPmState,
  pmPresence,
  publishPmPresence,
  refreshPmPresence,
  resetPmPresence,
  startPmPresencePoll,
} from "../../src/lib/pm/presence.js";

const AT = "2026-10-09T12:00:00.000Z";

/** A client that answers `GET /pm/presence` the way core does. */
const presenceClient = (presence) => ({
  getPmPresence: vi.fn(async () => presence),
});
const onboarded = (connected) => ({
  state: connected ? "connected" : "offline",
  last_seen: AT,
  runner: "Hermes",
  host: "studio",
  configured: true,
  connected,
});

afterEach(() => {
  resetPmPresence();
  vi.useRealTimers();
});

describe("reading PM state", () => {
  it("reads core's presence answer", async () => {
    await expect(
      fetchPmState({ client: presenceClient(onboarded(true)) }),
    ).resolves.toEqual({
      state: PM_STATES.CONNECTED,
      lastSeen: AT,
      runner: "Hermes",
      host: "studio",
    });
  });

  /*
   * A core older than the presence route cannot be asked, and that is not
   * evidence that no PM exists.
   */
  it("answers unknown when the client has no presence read", async () => {
    await expect(fetchPmState({ client: {} })).resolves.toMatchObject({
      state: PM_STATES.UNKNOWN,
    });
  });
});

describe("refreshing PM state", () => {
  it("publishes the state against its workspace", async () => {
    const result = await refreshPmPresence("ops", {
      client: presenceClient(onboarded(false)),
    });
    expect(result.read).toBe(true);
    expect(get(pmPresence)).toMatchObject({
      workspace: "ops",
      state: PM_STATES.OFFLINE,
      loaded: true,
    });
  });

  /*
   * An unreadable state is not "no PM": hiding Ask PM because one request
   * failed would break a workspace whose PM is running.
   */
  it("keeps the last known state when the read fails", async () => {
    await refreshPmPresence("ops", {
      client: presenceClient(onboarded(true)),
    });
    const failed = await refreshPmPresence("ops", {
      client: {
        getPmPresence: vi.fn(async () => {
          throw new Error("core unreachable");
        }),
      },
      force: true,
    });
    // The caller is told the read failed; the state is not changed.
    expect(failed.read).toBe(false);
    expect(get(pmPresence)).toMatchObject({
      workspace: "ops",
      state: PM_STATES.CONNECTED,
    });
  });

  it("drops another workspace's answer rather than showing it here", async () => {
    await refreshPmPresence("ops", {
      client: presenceClient(onboarded(true)),
    });
    const slow = refreshPmPresence("other", {
      client: {
        getPmPresence: async () => {
          clearPmPresence();
          pmPresence.set({
            workspace: "ops",
            state: PM_STATES.CONNECTED,
            lastSeen: "",
            runner: "",
            host: "",
            loaded: true,
          });
          return { state: "not_onboarded" };
        },
      },
    });
    await slow;
    expect(get(pmPresence)).toMatchObject({
      workspace: "ops",
      state: PM_STATES.CONNECTED,
    });
  });

  it("honours a minimum interval, and force overrides it", async () => {
    const client = presenceClient(onboarded(true));
    await refreshPmPresence("ops", { client });
    await refreshPmPresence("ops", {
      client,
      minIntervalMs: SHELL_REFRESH_MIN_MS,
    });
    expect(client.getPmPresence).toHaveBeenCalledTimes(1);
    await refreshPmPresence("ops", { client, force: true });
    expect(client.getPmPresence).toHaveBeenCalledTimes(2);
  });

  it("ignores a blank workspace", async () => {
    const client = presenceClient(onboarded(true));
    await refreshPmPresence("  ", { client });
    expect(client.getPmPresence).not.toHaveBeenCalled();
  });
});

describe("recording a state the caller already has", () => {
  it("publishes it without a request", () => {
    publishPmPresence("ops", PM_STATES.NOT_ONBOARDED);
    expect(get(pmPresence)).toMatchObject({
      workspace: "ops",
      state: PM_STATES.NOT_ONBOARDED,
      loaded: true,
    });
  });

  it("refuses to publish an unknown state over a known one", () => {
    publishPmPresence("ops", PM_STATES.CONNECTED);
    publishPmPresence("ops", { state: "nonsense" });
    expect(get(pmPresence)).toMatchObject({ state: PM_STATES.CONNECTED });
  });
});

describe("waiting for a first heartbeat", () => {
  it("polls until a PM connects, then stops", async () => {
    vi.useFakeTimers();
    let answer = { state: "not_onboarded" };
    const client = { getPmPresence: vi.fn(async () => answer) };
    const stop = startPmPresencePoll("ops", { client, intervalMs: 1000 });
    await vi.advanceTimersByTimeAsync(0);
    expect(get(pmPresence)).toMatchObject({
      state: PM_STATES.NOT_ONBOARDED,
    });

    await vi.advanceTimersByTimeAsync(1000);
    expect(client.getPmPresence).toHaveBeenCalledTimes(2);

    answer = onboarded(true);
    await vi.advanceTimersByTimeAsync(1000);
    expect(get(pmPresence)).toMatchObject({ state: PM_STATES.CONNECTED });

    const callsAtConnect = client.getPmPresence.mock.calls.length;
    await vi.advanceTimersByTimeAsync(5000);
    expect(client.getPmPresence).toHaveBeenCalledTimes(callsAtConnect);
    stop();
  });

  /*
   * A failed read is indistinguishable from an older core's silence, and the
   * reader is waiting on a heartbeat, so retry a few times — then stop,
   * because a core with no presence route will never answer.
   */
  it("retries an unreadable state a bounded number of times, then stops", async () => {
    vi.useFakeTimers();
    const client = { getPmPresence: vi.fn(async () => undefined) };
    const stop = startPmPresencePoll("ops", { client, intervalMs: 1000 });
    await vi.advanceTimersByTimeAsync(30_000);
    expect(client.getPmPresence).toHaveBeenCalledTimes(UNREADABLE_POLL_LIMIT);
    stop();
  });

  it("keeps waiting when a read fails and recovers when it succeeds", async () => {
    vi.useFakeTimers();
    let fail = true;
    const client = {
      getPmPresence: vi.fn(async () => {
        if (fail) throw new Error("session not ready");
        return { state: "not_onboarded" };
      }),
    };
    const stop = startPmPresencePoll("ops", { client, intervalMs: 1000 });
    await vi.advanceTimersByTimeAsync(1000);
    fail = false;
    await vi.advanceTimersByTimeAsync(1000);
    // The failure did not end the poll, and the state is now known.
    expect(get(pmPresence)).toMatchObject({
      workspace: "ops",
      state: PM_STATES.NOT_ONBOARDED,
    });
    stop();
  });

  /*
   * Installing a PM is a few commands on the reader's own machine, so a long
   * silence means something needs looking at — and a page left open for a day
   * must not keep spending requests on a hope.
   */
  it("gives up after the watch window and says so once", async () => {
    vi.useFakeTimers();
    const client = presenceClient({ state: "not_onboarded" });
    const gaveUp = vi.fn();
    const stop = startPmPresencePoll("ops", {
      client,
      intervalMs: 1000,
      watchMs: 3000,
      onGaveUp: gaveUp,
    });
    await vi.advanceTimersByTimeAsync(3000);
    expect(gaveUp).toHaveBeenCalledTimes(1);
    const callsAtGiveUp = client.getPmPresence.mock.calls.length;
    await vi.advanceTimersByTimeAsync(20_000);
    expect(client.getPmPresence).toHaveBeenCalledTimes(callsAtGiveUp);
    expect(gaveUp).toHaveBeenCalledTimes(1);
    stop();
  });

  it("does not spend the watch window while the tab is hidden", async () => {
    vi.useFakeTimers();
    // This module runs in the browser; the unit environment has no document.
    let hidden = true;
    globalThis.document = {
      get hidden() {
        return hidden;
      },
    };
    try {
      const client = presenceClient({ state: "not_onboarded" });
      const gaveUp = vi.fn();
      const stop = startPmPresencePoll("ops", {
        client,
        intervalMs: 1000,
        watchMs: 3000,
        onGaveUp: gaveUp,
      });
      await vi.advanceTimersByTimeAsync(20_000);
      expect(client.getPmPresence).not.toHaveBeenCalled();
      expect(gaveUp).not.toHaveBeenCalled();
      hidden = false;
      await vi.advanceTimersByTimeAsync(1000);
      expect(client.getPmPresence).toHaveBeenCalled();
      stop();
    } finally {
      delete globalThis.document;
    }
  });

  it("spends nothing after it is stopped", async () => {
    vi.useFakeTimers();
    const client = presenceClient({ state: "not_onboarded" });
    const stop = startPmPresencePoll("ops", { client, intervalMs: 1000 });
    await vi.advanceTimersByTimeAsync(0);
    stop();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(client.getPmPresence).toHaveBeenCalledTimes(1);
  });
});
