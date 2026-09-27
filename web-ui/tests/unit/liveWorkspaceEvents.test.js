import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { liveWorkspaceEvents } from "../../src/lib/liveWorkspaceEvents.js";

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
    expect(fake.client.listEvents).toHaveBeenCalledWith({
      type: ["card_moved"],
      limit: 1,
    });
    expect(fake.calls[0]).toMatchObject({
      types: ["card_moved"],
      lastEventId: "evt-9",
    });
    const ts = new Date().toISOString();
    fake.emit({ id: "evt-10", type: "card_moved", ts });
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

  it("is a no-op for a client without a stream", () => {
    const stop = liveWorkspaceEvents({ client: {}, onChange: vi.fn() });
    expect(stop).toBeTypeOf("function");
    stop();
  });
});
