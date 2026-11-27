import { describe, expect, it, vi } from "vitest";

import {
  LIFECYCLE_CHUNK,
  isLifecycleOperation,
  sendLifecycle,
} from "../../src/lib/lifecycleBatchSend.js";

const bases = (ids) =>
  Object.fromEntries(
    ids.map((id) => [id, { archived_at: null, trashed_at: null }]),
  );

function eventEntry(ids, action = "trash", receipt) {
  return {
    operation: "events.lifecycle",
    request: {
      selected_bases: bases(ids),
      items: ids.map((id) => ({
        id,
        action,
        body: {
          actor_id: "actor-a",
          request_key: `key-${id}`,
          if_lifecycle: { archived_at: null, trashed_at: null },
          reason: "",
        },
      })),
    },
    ...(receipt ? { receipt } : {}),
  };
}

function docEntry(ids, action = "archive") {
  return {
    operation: "docs.lifecycle",
    request: {
      items: ids.map((id) => ({
        id,
        action,
        body: {
          actor_id: "actor-a",
          request_key: `key-${id}`,
          if_updated_at: "2026-11-01T00:00:00Z",
        },
      })),
    },
  };
}

const eventDone = (id, affected) => ({
  status: "done",
  http_status: 200,
  response: {
    event: { id },
    ...(affected ? { affected_event_ids: affected } : {}),
  },
});
const docDone = (id) => ({
  status: "done",
  http_status: 200,
  response: {
    document: { id, updated_at: "2026-11-02T00:00:00Z" },
    revision: { id: `rev-${id}` },
  },
});
const failed = (status, code, message = code) => ({
  status: "failed",
  http_status: status,
  error: { code, message },
});

function harness() {
  const checkpoints = [];
  return {
    checkpoints,
    io: {
      active: () => true,
      checkpoint: vi.fn(async (receipt) => {
        checkpoints.push(structuredClone(receipt));
      }),
    },
  };
}

describe("bulk lifecycle sender", () => {
  it("names only the two lifecycle operations", () => {
    expect(isLifecycleOperation("events.lifecycle")).toBe(true);
    expect(isLifecycleOperation("docs.lifecycle")).toBe(true);
    expect(isLifecycleOperation("inbox.skip_many")).toBe(false);
    expect(isLifecycleOperation("toString")).toBe(false);
  });

  it("sends a document selection as one batch and checkpoints once", async () => {
    const entry = docEntry(["d1", "d2", "d3"]);
    const client = {
      changeDocumentsLifecycle: vi.fn(async (items) => ({
        results: items.map((item) => docDone(item.id)),
      })),
      archiveDocument: vi.fn(),
    };
    const { io, checkpoints } = harness();
    const receipt = await sendLifecycle(client, entry, io);
    expect(client.changeDocumentsLifecycle).toHaveBeenCalledTimes(1);
    expect(client.archiveDocument).not.toHaveBeenCalled();
    // Each item is the single route's body plus its id and action.
    expect(client.changeDocumentsLifecycle.mock.calls[0][0][0]).toEqual({
      ...entry.request.items[0].body,
      id: "d1",
      action: "archive",
    });
    expect(checkpoints).toHaveLength(1);
    expect(receipt.documents.map((done) => done.id)).toEqual([
      "d1",
      "d2",
      "d3",
    ]);
    expect(receipt.documents[0].result.revision.id).toBe("rev-d1");
  });

  it("chunks a large selection and sends the event bases once per chunk", async () => {
    const ids = Array.from(
      { length: LIFECYCLE_CHUNK + 3 },
      (_, index) => `e${index}`,
    );
    const entry = eventEntry(ids);
    const client = {
      changeEventsLifecycle: vi.fn(async (items) => ({
        results: items.map((item) => eventDone(item.id)),
      })),
    };
    const { io, checkpoints } = harness();
    const receipt = await sendLifecycle(client, entry, io);
    expect(client.changeEventsLifecycle).toHaveBeenCalledTimes(2);
    const [firstItems, firstBases] = client.changeEventsLifecycle.mock.calls[0];
    expect(firstItems).toHaveLength(LIFECYCLE_CHUNK);
    expect(firstItems[0].selected_bases).toBeUndefined();
    expect(firstBases).toEqual(entry.request.selected_bases);
    expect(client.changeEventsLifecycle.mock.calls[1][0]).toHaveLength(3);
    expect(checkpoints).toHaveLength(2);
    expect(receipt.events).toHaveLength(ids.length);
  });

  it("skips items already receipted and items a reply cascade covered", async () => {
    const entry = eventEntry(["done", "parent", "reply", "other"], "trash", {
      events: [{ id: "done", result: { event: { id: "done" } } }],
    });
    const client = {
      changeEventsLifecycle: vi.fn(async () => ({
        results: [
          eventDone("parent", ["parent", "reply"]),
          // The reply's own attempt conflicts: the parent already trashed it.
          failed(409, "conflict"),
          eventDone("other"),
        ],
      })),
    };
    const { io } = harness();
    const receipt = await sendLifecycle(client, entry, io);
    expect(
      client.changeEventsLifecycle.mock.calls[0][0].map((item) => item.id),
    ).toEqual(["parent", "reply", "other"]);
    expect(receipt.events.map((done) => done.id)).toEqual([
      "done",
      "parent",
      "reply",
      "other",
    ]);
    expect(receipt.events[2].result.event.id).toBe("parent");
  });

  it("keeps receipts for the successes and fails like the single route", async () => {
    const entry = docEntry(["d1", "d2", "d3"], "trash");
    const client = {
      changeDocumentsLifecycle: vi.fn(async () => ({
        results: [
          docDone("d1"),
          failed(
            409,
            "conflict",
            "document has been updated; refresh and retry",
          ),
          docDone("d3"),
        ],
      })),
    };
    const { io, checkpoints } = harness();
    const error = await sendLifecycle(client, entry, io).catch((e) => e);
    expect(error.status).toBe(409);
    expect(error.body.error.code).toBe("conflict");
    expect(error.message).toContain("refresh and retry");
    expect(checkpoints).toHaveLength(1);
    expect(entry.receipt.documents.map((done) => done.id)).toEqual([
      "d1",
      "d3",
    ]);

    // The retry sends only what is left; a lone item takes its single route
    // with the body it was saved with.
    client.trashDocument = vi.fn(async (id) => docDone(id).response);
    await sendLifecycle(client, entry, io);
    expect(client.changeDocumentsLifecycle).toHaveBeenCalledTimes(1);
    expect(client.trashDocument).toHaveBeenCalledWith(
      "d2",
      entry.request.items[1].body,
    );
    expect(entry.receipt.documents.map((done) => done.id)).toEqual([
      "d1",
      "d3",
      "d2",
    ]);
  });

  it("sends deferred items again in the next request", async () => {
    const entry = docEntry(["d1", "d2", "d3"]);
    const client = {
      changeDocumentsLifecycle: vi
        .fn()
        .mockResolvedValueOnce({
          results: [
            docDone("d1"),
            { index: 1, status: "deferred" },
            { index: 2, status: "deferred" },
          ],
        })
        .mockResolvedValueOnce({ results: [docDone("d2"), docDone("d3")] }),
    };
    const { io } = harness();
    const receipt = await sendLifecycle(client, entry, io);
    expect(client.changeDocumentsLifecycle).toHaveBeenCalledTimes(2);
    expect(
      client.changeDocumentsLifecycle.mock.calls[1][0].map((item) => item.id),
    ).toEqual(["d2", "d3"]);
    expect(receipt.documents).toHaveLength(3);
  });

  it("retries later when nothing in a chunk was attempted", async () => {
    const entry = docEntry(["d1", "d2"]);
    const client = {
      changeDocumentsLifecycle: vi.fn(async () => ({
        results: [
          { index: 0, status: "deferred" },
          { index: 1, status: "deferred" },
        ],
      })),
    };
    const { io } = harness();
    const error = await sendLifecycle(client, entry, io).catch((e) => e);
    expect(error.status).toBe(503);
    expect(entry.receipt).toBeUndefined();
  });

  it("treats a malformed batch response as uncertain", async () => {
    const entry = docEntry(["d1", "d2"]);
    const client = {
      changeDocumentsLifecycle: vi.fn(async () => ({
        results: [docDone("d1")],
      })),
    };
    const { io } = harness();
    await expect(sendLifecycle(client, entry, io)).rejects.toThrow(
      "Document lifecycle receipt is incomplete.",
    );
  });

  it.each([
    ["an older core", 404, { error: { message: "endpoint not found" } }],
    [
      "a hosted proxy",
      404,
      { error: { code: "route_not_available", message: "not available" } },
    ],
    ["a core without POST", 405, { error: { message: "method not allowed" } }],
  ])("falls back to one send per item on %s", async (_name, status, body) => {
    const entry = eventEntry(["parent", "reply", "other"]);
    const client = {
      changeEventsLifecycle: vi.fn(async () => {
        throw Object.assign(new Error("no route"), { status, body });
      }),
      trashEvent: vi.fn(async (id) =>
        id === "parent"
          ? { event: { id }, affected_event_ids: ["parent", "reply"] }
          : { event: { id } },
      ),
    };
    const { io, checkpoints } = harness();
    const receipt = await sendLifecycle(client, entry, io);
    // The reply rode its parent's cascade, as it always has.
    expect(client.trashEvent.mock.calls.map(([id]) => id)).toEqual([
      "parent",
      "other",
    ]);
    expect(client.trashEvent.mock.calls[0][1]).toEqual({
      ...entry.request.items[0].body,
      selected_bases: entry.request.selected_bases,
    });
    expect(checkpoints).toHaveLength(2);
    expect(receipt.events.map((done) => done.id)).toEqual([
      "parent",
      "reply",
      "other",
    ]);
  });

  it("does not fall back on other batch failures", async () => {
    const entry = docEntry(["d1", "d2"]);
    const client = {
      changeDocumentsLifecycle: vi.fn(async () => {
        throw Object.assign(new Error("not found"), {
          status: 404,
          body: {
            error: { code: "not_found", message: "workspace not found" },
          },
        });
      }),
      archiveDocument: vi.fn(),
    };
    const { io } = harness();
    const error = await sendLifecycle(client, entry, io).catch((e) => e);
    expect(error.status).toBe(404);
    expect(client.archiveDocument).not.toHaveBeenCalled();
  });

  it("stops when the submitting identity is no longer active", async () => {
    const entry = docEntry(["d1"]);
    const client = { changeDocumentsLifecycle: vi.fn() };
    const { io } = harness();
    await expect(
      sendLifecycle(client, entry, { ...io, active: () => false }),
    ).rejects.toThrow("no longer active");
    expect(client.changeDocumentsLifecycle).not.toHaveBeenCalled();
  });

  it("refuses unknown actions and legacy event selections without bases", async () => {
    const { io } = harness();
    const unknown = docEntry(["d1"], "purge");
    await expect(sendLifecycle({}, unknown, io)).rejects.toMatchObject({
      status: 400,
    });
    const legacy = eventEntry(["e1", "e2"]);
    delete legacy.request.selected_bases;
    await expect(sendLifecycle({}, legacy, io)).rejects.toMatchObject({
      publicMessage: expect.stringContaining("reply versions"),
    });
  });
});
