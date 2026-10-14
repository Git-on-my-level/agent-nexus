import { describe, expect, it, vi } from "vitest";
import { createSharedReadFetch } from "../../src/lib/sharedReadFetch.js";

const deferred = () => {
  let resolve, reject;
  const promise = new Promise((a, b) => {
    resolve = a;
    reject = b;
  });
  return { promise, resolve, reject };
};

describe("shared finite reads", () => {
  it("shares matching reads across clients, clones bodies, and never caches completed reads", async () => {
    const gate = deferred();
    const fetch = vi
      .fn()
      .mockImplementationOnce(() => gate.promise)
      .mockImplementation(async () => new Response('{"items":[]}'));
    const read = createSharedReadFetch();
    const a = read(fetch, "/inbox?limit=50");
    const b = read(fetch, "/inbox?limit=50");
    await Promise.resolve();
    expect(fetch).toHaveBeenCalledTimes(1);
    gate.resolve(new Response('{"items":[]}'));
    const responses = await Promise.all([a, b]);
    expect(await responses[0].json()).toEqual({ items: [] });
    expect(await responses[1].json()).toEqual({ items: [] });
    await read(fetch, "/inbox?limit=50");
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("isolates workspace, identity, credentials and query keys and bypasses writes and streams", async () => {
    let epoch = 1;
    const read = createSharedReadFetch({ scope: () => epoch });
    const fetch = vi.fn(async () => new Response("ok"));
    const reads = [read(fetch, "/inbox", { headers: { "x-workspace": "a" } })];
    reads.push(read(fetch, "/inbox", { headers: { "x-workspace": "b" } }));
    reads.push(read(fetch, "/inbox?cursor=next"));
    reads.push(
      read(fetch, "/inbox", { headers: { authorization: "Bearer other" } }),
    );
    epoch++;
    reads.push(read(fetch, "/inbox", { headers: { "x-workspace": "a" } }));
    for (let i = 0; i < 2; i++) {
      reads.push(read(fetch, "/inbox/respond", { method: "POST" }));
      reads.push(read(fetch, "/stream/inbox"));
    }
    await Promise.all(reads);
    expect(fetch).toHaveBeenCalledTimes(9);
  });

  it("cancels just one subscriber and aborts transport when every subscriber leaves", async () => {
    const gate = deferred();
    const fetch = vi.fn(() => gate.promise);
    const read = createSharedReadFetch();
    const first = new AbortController(),
      second = new AbortController();
    const a = read(fetch, "/events", { signal: first.signal });
    const b = read(fetch, "/events", { signal: second.signal });
    const rejected = expect(a).rejects.toBeDefined();
    await Promise.resolve();
    first.abort();
    await rejected;
    expect(fetch.mock.calls[0][1].signal.aborted).toBe(false);
    gate.resolve(new Response("still readable"));
    expect(await (await b).text()).toBe("still readable");
    const c = read(fetch, "/work", { signal: second.signal });
    const cancelled = expect(c).rejects.toBeDefined();
    await Promise.resolve();
    second.abort();
    await cancelled;
    expect(fetch.mock.calls[1][1].signal.aborted).toBe(true);
  });

  it("evicts rejected reads so a retry can succeed", async () => {
    const read = createSharedReadFetch();
    const fetch = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockResolvedValue(new Response("ok"));
    await expect(read(fetch, "/agents")).rejects.toThrow("offline");
    expect(await (await read(fetch, "/agents")).text()).toBe("ok");
  });
});

it("keeps cancellation and sharing active after headers while a finite body stalls", async () => {
  let finish;
  const body = new ReadableStream({
    start(controller) {
      finish = () => {
        controller.enqueue(new TextEncoder().encode('{"ok":true}'));
        controller.close();
      };
    },
  });
  const fetch = vi.fn(async () => new Response(body));
  const read = createSharedReadFetch();
  const controller = new AbortController();
  const a = read(fetch, "/events", { signal: controller.signal });
  await Promise.resolve();
  const b = read(fetch, "/events");
  const rejected = expect(a).rejects.toBeDefined();
  controller.abort();
  await rejected;
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(fetch.mock.calls[0][1].signal.aborted).toBe(false);
  finish();
  expect(await (await b).json()).toEqual({ ok: true });
});
