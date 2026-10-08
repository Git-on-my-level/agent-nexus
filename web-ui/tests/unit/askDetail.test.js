import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  invalidateAskDetail,
  loadAskDetail,
  resetAskDetailCache,
} from "../../src/lib/askDetail.js";
import { selectedActorId } from "../../src/lib/actorSession.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";

/**
 * The read path behind the Inbox's ask panels: two bounded point reads for the
 * selected item, cached inside the reader's own scope and only for as long as
 * delivery state is plausibly unchanged.
 */

const ITEM = { id: "inbox-1", request_event_ref: "event:ask-1" };

function stubClient(overrides = {}) {
  return {
    getAsk: vi.fn(async () => ({ ask_id: "event:ask-1", status: "answered" })),
    getEvent: vi.fn(async () => ({ event: { id: "ask-1", payload: {} } })),
    ...overrides,
  };
}

beforeEach(() => {
  resetAskDetailCache();
  currentOrganizationSlug.set("acme");
  currentWorkspaceSlug.set("ops");
  selectedActorId.set("actor-a");
});

afterEach(() => {
  vi.useRealTimers();
  resetAskDetailCache();
});

describe("loadAskDetail", () => {
  it("reads the outcome and the ask event, keyed by the item's request ref", async () => {
    const client = stubClient();
    const value = await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledWith("event:ask-1");
    // `events.get` takes the bare id, not the ref.
    expect(client.getEvent).toHaveBeenCalledWith("ask-1");
    expect(value).toMatchObject({
      askRef: "event:ask-1",
      outcome: { status: "answered" },
      event: { id: "ask-1" },
    });
  });

  it("asks for nothing when the item names no ask", async () => {
    const client = stubClient();
    expect(await loadAskDetail({ id: "inbox-2" }, { client })).toEqual({
      askRef: "",
      outcome: null,
      event: null,
    });
    expect(client.getAsk).not.toHaveBeenCalled();
    expect(client.getEvent).not.toHaveBeenCalled();
  });

  it("serves one pair of reads to concurrent and repeat callers", async () => {
    const client = stubClient();
    await Promise.all([
      loadAskDetail(ITEM, { client }),
      loadAskDetail(ITEM, { client }),
    ]);
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(1);
    expect(client.getEvent).toHaveBeenCalledTimes(1);
  });

  it("re-reads once the cached read is no longer plausibly current", async () => {
    vi.useFakeTimers();
    const client = stubClient();
    await loadAskDetail(ITEM, { client });
    vi.setSystemTime(Date.now() + 29_000);
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(1);
    vi.setSystemTime(Date.now() + 2_000);
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(2);
  });

  it("keeps a refused read out of the cache and falls back to nulls", async () => {
    const client = stubClient({
      getAsk: vi.fn(async () => {
        throw Object.assign(new Error("not found"), { status: 404 });
      }),
      getEvent: vi.fn(async () => {
        throw Object.assign(new Error("forbidden"), { status: 403 });
      }),
    });
    expect(await loadAskDetail(ITEM, { client })).toEqual({
      askRef: "event:ask-1",
      outcome: null,
      event: null,
    });
    // Nothing was cached, so a later read tries again rather than waiting out
    // the window: both calls can fail for reasons that pass.
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(2);
  });

  it("caches a partial read — a refused event still leaves the outcome", async () => {
    const client = stubClient({
      getEvent: vi.fn(async () => {
        throw new Error("no");
      }),
    });
    const value = await loadAskDetail(ITEM, { client });
    expect(value.outcome).toMatchObject({ status: "answered" });
    expect(value.event).toBeNull();
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(1);
  });

  it("never serves one reader's read to another", async () => {
    const client = stubClient();
    await loadAskDetail(ITEM, { client });
    selectedActorId.set("actor-b");
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(2);
  });

  it("never serves one workspace's read to another", async () => {
    const client = stubClient();
    await loadAskDetail(ITEM, { client });
    currentWorkspaceSlug.set("other");
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(2);
  });
});

describe("invalidateAskDetail", () => {
  it("drops one ask, which is what an answer invalidates", async () => {
    const client = stubClient();
    const other = { id: "inbox-9", request_event_ref: "event:ask-9" };
    await loadAskDetail(ITEM, { client });
    await loadAskDetail(other, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(2);
    invalidateAskDetail(ITEM);
    await loadAskDetail(ITEM, { client });
    await loadAskDetail(other, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(3);
    expect(client.getAsk).toHaveBeenLastCalledWith("event:ask-1");
  });

  it("drops everything when given nothing", async () => {
    const client = stubClient();
    await loadAskDetail(ITEM, { client });
    invalidateAskDetail();
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(2);
  });

  it("tolerates an item that names no ask", async () => {
    const client = stubClient();
    await loadAskDetail(ITEM, { client });
    invalidateAskDetail({ id: "inbox-2" });
    await loadAskDetail(ITEM, { client });
    expect(client.getAsk).toHaveBeenCalledTimes(1);
  });
});
