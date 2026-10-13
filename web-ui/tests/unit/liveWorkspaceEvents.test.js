import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  liveAgentChanges,
  liveInboxChanges,
  liveWorkspaceEvents,
} from "../../src/lib/liveWorkspaceEvents.js";

function fakeClient({ newest = "evt-9" } = {}) {
  const calls = [];
  let emit = null;
  let fail = null;
  const client = {
    listEvents: vi.fn(async () => ({ events: newest ? [{ id: newest }] : [] })),
    streamEvents: vi.fn(
      (options) =>
        new Promise((resolve, reject) => {
          calls.push(options);
          emit = options.onEvent;
          fail = reject;
          options.signal?.addEventListener("abort", () => {
            const error = new Error("aborted");
            error.name = "AbortError";
            reject(error);
          });
        }),
    ),
  };
  return {
    client,
    calls,
    emit: (event, id = event.id) =>
      emit({ id, event: "event", data: { event } }),
    fail: (error) => fail(error),
  };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("liveWorkspaceEvents", () => {
  beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
  afterEach(() => vi.useRealTimers());

  it("shares inbox progress, resumes the keyset boundary, and clears partial state", async () => {
    let emit;
    let fail;
    const client = {
      streamInboxItems: vi.fn(
        (options) =>
          new Promise((resolve, reject) => {
            emit = options.onEvent;
            fail = reject;
            options.signal.addEventListener("abort", () =>
              reject(
                Object.assign(new Error("aborted"), { name: "AbortError" }),
              ),
            );
          }),
      ),
      listEvents: vi.fn(),
    };
    const page = vi.fn();
    const badge = vi.fn();
    const stopPage = liveInboxChanges({
      client,
      onChange: page,
      reconnectMs: 10,
    });
    const stopBadge = liveInboxChanges({ client, onChange: badge });
    await flush();
    emit({
      id: "item:one",
      event: "inbox_item",
      data: { item: { id: "one" } },
    });
    emit({
      id: "inbox-page:cursor",
      event: "inbox_page",
      data: { partial: true, resume_cursor: "cursor" },
    });
    await vi.advanceTimersByTimeAsync(300);
    expect(page.mock.calls[0][0]).toEqual([
      { type: "inbox_item", item: { id: "one" } },
      { type: "inbox_page", partial: true, resume_cursor: "cursor" },
    ]);
    expect(badge).toHaveBeenCalledTimes(1);
    expect(client.streamInboxItems).toHaveBeenCalledTimes(1);
    expect(client.listEvents).not.toHaveBeenCalled();
    fail(new Error("dropped"));
    await vi.advanceTimersByTimeAsync(50);
    expect(client.streamInboxItems.mock.calls[1][0].lastEventId).toBe(
      "inbox-page:cursor",
    );
    emit({
      id: "inbox-page:",
      event: "inbox_page",
      data: { partial: false, resume_cursor: "" },
    });
    await vi.advanceTimersByTimeAsync(300);
    expect(page.mock.calls[1][0]).toEqual([
      { type: "inbox_page", partial: false, resume_cursor: "" },
    ]);
    fail(new Error("dropped"));
    await vi.advanceTimersByTimeAsync(50);
    expect(client.streamInboxItems.mock.calls[2][0].lastEventId).toBe(
      "inbox-page:",
    );
    stopPage();
    expect(client.streamInboxItems.mock.calls[2][0].signal.aborted).toBe(false);
    stopBadge();
    expect(client.streamInboxItems.mock.calls[2][0].signal.aborted).toBe(true);
  });

  it("starts after the newest matching event and coalesces a burst", async () => {
    const fake = fakeClient();
    const onChange = vi.fn();
    const stop = liveWorkspaceEvents({
      client: fake.client,
      types: ["card_moved"],
      onChange,
    });
    await flush();
    await flush();
    expect(fake.client.listEvents).toHaveBeenCalledWith({ limit: 1 });
    expect(fake.calls[0]).toMatchObject({ lastEventId: "evt-9" });
    const ts = new Date().toISOString();
    fake.emit({ id: "evt-10", type: "card_moved", ts });
    fake.emit({ id: "evt-10b", type: "document_created", ts });
    fake.emit({ id: "evt-11", type: "card_moved", ts });
    expect(onChange).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(500);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange.mock.calls[0][0].map((event) => event.id)).toEqual([
      "evt-10",
      "evt-11",
    ]);
    stop();
  });

  it("flushes a sustained stream on its max wait instead of waiting for quiet", async () => {
    // Timers only move when this test moves them, so the gaps below are the
    // module's own scheduling, not the runner's wall clock. Reconfiguring the
    // suite's clock needs the real one back first.
    vi.useRealTimers();
    vi.useFakeTimers({ shouldAdvanceTime: false });
    const fake = fakeClient({ newest: "" });
    const flushes = [];
    const stop = liveWorkspaceEvents({
      client: fake.client,
      debounceMs: 400,
      maxWaitMs: 1500,
      onChange: (batch) => flushes.push({ at: Date.now(), size: batch.length }),
    });
    await vi.advanceTimersByTimeAsync(1);
    await vi.advanceTimersByTimeAsync(1);
    // An event every 25 ms for 5 s: the debounce timer never gets its silence.
    const streamStart = Date.now();
    for (let i = 0; i < 200; i++) {
      fake.emit({
        id: `evt-${i}`,
        type: "card_moved",
        ts: new Date().toISOString(),
      });
      await vi.advanceTimersByTimeAsync(25);
    }
    const duringStream = flushes.length;
    // The tail of the stream lands on the ordinary debounce once it stops.
    await vi.advanceTimersByTimeAsync(500);
    // Nothing waited for the stream to stop: 5 s of events arrived in flushes
    // whose spacing never exceeds the max wait.
    expect(duringStream).toBeGreaterThanOrEqual(3);
    let previous = streamStart;
    for (const { at } of flushes) {
      expect(at - previous).toBeLessThanOrEqual(1500);
      previous = at;
    }
    expect(flushes.reduce((total, { size }) => total + size, 0)).toBe(200);
    stop();
  });

  it("still debounces a burst up to its max wait", async () => {
    const fake = fakeClient({ newest: "" });
    const onChange = vi.fn();
    const stop = liveWorkspaceEvents({
      client: fake.client,
      debounceMs: 400,
      maxWaitMs: 1500,
      onChange,
    });
    await flush();
    await flush();
    for (let i = 0; i < 3; i++) {
      fake.emit({
        id: `burst-${i}`,
        type: "card_moved",
        ts: new Date().toISOString(),
      });
      await vi.advanceTimersByTimeAsync(50);
    }
    // Still inside the debounce window, and well inside the max wait.
    expect(onChange).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(400);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange.mock.calls[0][0]).toHaveLength(3);
    stop();
  });

  it("keeps one refresh in flight and flushes what arrived behind it", async () => {
    const fake = fakeClient({ newest: "" });
    let release = null;
    const batches = [];
    let inFlight = 0;
    let overlapped = false;
    const stop = liveWorkspaceEvents({
      client: fake.client,
      debounceMs: 100,
      maxWaitMs: 300,
      onChange: (batch) => {
        if (inFlight > 0) overlapped = true;
        inFlight++;
        batches.push(batch.map((event) => event.id));
        return new Promise((resolve) => {
          release = () => {
            inFlight--;
            resolve();
          };
        });
      },
    });
    await flush();
    await flush();
    fake.emit({ id: "a", type: "card_moved", ts: new Date().toISOString() });
    await vi.advanceTimersByTimeAsync(150);
    expect(batches).toEqual([["a"]]);
    // The refresh is still running: later events queue instead of starting
    // a second one, however long the stream stays busy.
    for (const id of ["b", "c", "d"]) {
      fake.emit({ id, type: "card_moved", ts: new Date().toISOString() });
      await vi.advanceTimersByTimeAsync(200);
    }
    expect(batches).toHaveLength(1);
    expect(overlapped).toBe(false);
    const finishFirst = release;
    finishFirst();
    await vi.advanceTimersByTimeAsync(10);
    // Past its max wait while it waited, so the queue flushes at once.
    expect(batches).toEqual([["a"], ["b", "c", "d"]]);
    release();
    await vi.advanceTimersByTimeAsync(10);
    expect(batches).toHaveLength(2);
    expect(overlapped).toBe(false);
    stop();
  });

  it("bounds the queue behind a refresh that never settles", async () => {
    const fake = fakeClient({ newest: "" });
    const batches = [];
    const stop = liveWorkspaceEvents({
      client: fake.client,
      debounceMs: 10,
      maxWaitMs: 20,
      onChange: (batch) => {
        batches.push(batch);
        return new Promise(() => {});
      },
    });
    await flush();
    await flush();
    fake.emit({
      id: "first",
      type: "card_moved",
      ts: new Date().toISOString(),
    });
    await vi.advanceTimersByTimeAsync(30);
    expect(batches).toHaveLength(1);
    for (let i = 0; i < 600; i++) {
      fake.emit({
        id: `queued-${i}`,
        type: "card_moved",
        ts: new Date().toISOString(),
      });
    }
    await vi.advanceTimersByTimeAsync(100);
    // Only the newest 500 are held; the oldest are dropped rather than grown
    // without limit, and they describe state the next re-read sees anyway.
    stop();
    const stopped = liveWorkspaceEvents({
      client: fake.client,
      debounceMs: 10,
      maxWaitMs: 20,
      onChange: (batch) => batches.push(batch),
    });
    await flush();
    await flush();
    for (let i = 0; i < 600; i++) {
      fake.emit({
        id: `probe-${i}`,
        type: "card_moved",
        ts: new Date().toISOString(),
      });
    }
    await vi.advanceTimersByTimeAsync(30);
    const probed = batches[batches.length - 1];
    expect(probed).toHaveLength(500);
    expect(probed[0].id).toBe("probe-100");
    expect(probed[499].id).toBe("probe-599");
    stopped();
  });

  it("ignores replayed history and events the filter rejects", async () => {
    const fake = fakeClient({ newest: "" });
    const onChange = vi.fn();
    const stop = liveWorkspaceEvents({
      client: fake.client,
      filter: (event) => event.refs?.includes("card:mine"),
      onChange,
    });
    await flush();
    await flush();
    fake.emit({ id: "old", ts: "2020-01-01T00:00:00Z", refs: ["card:mine"] });
    fake.emit({
      id: "other",
      ts: new Date().toISOString(),
      refs: ["card:other"],
    });
    await vi.advanceTimersByTimeAsync(500);
    expect(onChange).not.toHaveBeenCalled();
    stop();
  });

  it("resumes from the last seen event and stops for good on 401", async () => {
    const fake = fakeClient();
    const stop = liveWorkspaceEvents({
      client: fake.client,
      onChange: () => {},
      reconnectMs: 10,
    });
    await flush();
    await flush();
    fake.emit({ id: "evt-12", ts: new Date().toISOString() });
    fake.fail(new Error("dropped"));
    await vi.advanceTimersByTimeAsync(50);
    expect(fake.calls[1].lastEventId).toBe("evt-12");
    const unauthorized = new Error("unauthorized");
    unauthorized.status = 401;
    fake.fail(unauthorized);
    await vi.advanceTimersByTimeAsync(5_000);
    expect(fake.calls).toHaveLength(2);
    stop();
  });

  it("shares one connection per scope and closes it with the last subscriber", async () => {
    const fake = fakeClient();
    const tasks = vi.fn();
    const inbox = vi.fn();
    const stopTasks = liveWorkspaceEvents({
      client: fake.client,
      types: ["card_moved"],
      onChange: tasks,
    });
    const stopInbox = liveWorkspaceEvents({
      client: fake.client,
      onChange: inbox,
    });
    await flush();
    await flush();
    expect(fake.client.streamEvents).toHaveBeenCalledTimes(1);
    fake.emit({
      id: "evt-20",
      type: "message_posted",
      ts: new Date().toISOString(),
    });
    await vi.advanceTimersByTimeAsync(500);
    expect(tasks).not.toHaveBeenCalled();
    expect(inbox).toHaveBeenCalledTimes(1);
    stopTasks();
    const [{ signal }] = fake.client.streamEvents.mock.calls[0];
    expect(signal.aborted).toBe(false);
    stopInbox();
    expect(signal.aborted).toBe(true);
    const again = liveWorkspaceEvents({
      client: fake.client,
      onChange: vi.fn(),
    });
    await flush();
    await flush();
    expect(fake.client.streamEvents).toHaveBeenCalledTimes(2);
    again();
  });

  it("keeps a thread subscription on its own connection", async () => {
    const fake = fakeClient();
    const stopWorkspace = liveWorkspaceEvents({
      client: fake.client,
      onChange: vi.fn(),
    });
    const stopThread = liveWorkspaceEvents({
      client: fake.client,
      threadId: "thread-1",
      onChange: vi.fn(),
    });
    await flush();
    await flush();
    expect(fake.calls.map((call) => call.threadId)).toEqual([
      undefined,
      "thread-1",
    ]);
    stopWorkspace();
    stopThread();
  });

  it("is a no-op for a client without a stream", () => {
    const stop = liveWorkspaceEvents({ client: {}, onChange: vi.fn() });
    expect(stop).toBeTypeOf("function");
    stop();
  });

  it("shares one roster change stream and notifies on connect and changes", async () => {
    let emit = null;
    let fail = null;
    const client = {
      streamAgentChanges: vi.fn(
        ({ onEvent, signal }) =>
          new Promise((resolve, reject) => {
            emit = onEvent;
            fail = reject;
            signal?.addEventListener("abort", () => {
              const error = new Error("aborted");
              error.name = "AbortError";
              reject(error);
            });
          }),
      ),
    };
    const badge = vi.fn();
    const page = vi.fn();
    // The first subscriber opens the shared connection and sets its backoff.
    const stopBadge = liveAgentChanges({
      client,
      onChange: badge,
      reconnectMs: 10,
    });
    const stopPage = liveAgentChanges({ client, onChange: page });
    await flush();
    expect(client.streamAgentChanges).toHaveBeenCalledTimes(1);
    // Core's first notification on connect is itself a change.
    emit({ event: "agents_changed", data: { revision: 1 } });
    emit({ event: "agents_changed", data: { revision: 2 } });
    await vi.advanceTimersByTimeAsync(300);
    expect(badge).toHaveBeenCalledTimes(1);
    expect(page.mock.calls[0][0].map((change) => change.revision)).toEqual([
      1, 2,
    ]);
    // A dropped stream reconnects; its first notification refetches again.
    fail(new Error("dropped"));
    await vi.advanceTimersByTimeAsync(50);
    expect(client.streamAgentChanges).toHaveBeenCalledTimes(2);
    emit({ event: "agents_changed", data: { revision: 3 } });
    await vi.advanceTimersByTimeAsync(300);
    expect(badge).toHaveBeenCalledTimes(2);
    stopBadge();
    stopPage();
  });

  it("is a no-op for a client without the roster stream", () => {
    const stop = liveAgentChanges({ client: {}, onChange: vi.fn() });
    expect(stop).toBeTypeOf("function");
    stop();
  });
});
