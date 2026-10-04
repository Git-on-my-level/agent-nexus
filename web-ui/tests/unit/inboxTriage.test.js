// @vitest-environment jsdom
import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const client = vi.hoisted(() => ({ respondInboxItem: vi.fn() }));
vi.mock("$lib/coreClient", () => ({ coreClient: client }));

import {
  eventPhraseParts,
  shortActorName,
  updateDigest,
} from "../../src/lib/inboxDigest.js";
import {
  UNDO_WINDOW_MS,
  applyResponseOverlay,
  defaultNotifyMode,
  flushInboxResponse,
  inboxResponseOverlay,
  inboxResponseToast,
  queueInboxResponse,
  retryInboxResponse,
  resetInboxResponseQueue,
  undoInboxResponse,
} from "../../src/lib/inboxResponseQueue.js";
import {
  inboxShortcutAction,
  isTextEntryTarget,
} from "../../src/lib/inboxShortcuts.js";
import {
  contextThreads,
  pickProgressNote,
} from "../../src/lib/inboxContext.js";

function key(k, extra = {}) {
  return {
    key: k,
    target: document.body,
    defaultPrevented: false,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...extra,
  };
}

describe("update digests", () => {
  const names = {
    "actor-leo": "Leo Park",
    "actor-nina": "Nina Vale",
    "actor-maya": "Maya Chen (Studio producer)",
  };
  const actorName = (id) => names[id] || "";

  it("groups by person and verb, newest person first", () => {
    const digest = updateDigest(
      [
        {
          type: "card_created",
          actor_id: "actor-leo",
          summary: "Card created: Parry tuning",
          refs: ["card:parry"],
        },
        {
          type: "document_revised",
          actor_id: "actor-nina",
          payload: { subject_title: "UI kit notes" },
          refs: ["document:ui-kit"],
        },
        {
          type: "message_posted",
          actor_id: "actor-leo",
          payload: { text: "x" },
        },
      ],
      { actorName },
    );
    expect(digest).toBe(
      "Leo created Parry tuning and commented · Nina revised UI kit notes",
    );
  });

  it("preserves older asks, answers and phase transitions after routine edits, including other actors", () => {
    const event = (type, actor_id = "claude", payload = {}) => ({
      type,
      actor_id,
      payload,
      refs: ["card:pilot"],
    });
    const digest = updateDigest(
      [
        event("card_updated"),
        event("card_created"),
        event("document_revised"),
        event("human_attention_requested", "claude", { kind: "ask" }),
        event("human_attention_responded"),
        event("card_moved", "claude", { column_key: "blocked" }),
        event("card_moved", "david", { column_key: "done" }),
      ],
      { actorName: (id) => id, maxActors: 1 },
    );
    expect(digest).toContain("asked");
    expect(digest).toContain("answered an ask");
    expect(digest).toContain("to blocked");
    expect(digest).toContain("david moved");
    expect(digest).toContain("to done");
    expect(digest.indexOf("answered an ask")).toBeLessThan(
      digest.indexOf("updated"),
    );
  });

  it("calls the reader You and drops a role from a name", () => {
    const events = [
      { type: "card_updated", actor_id: "actor-maya", refs: ["card:a"] },
      { type: "card_updated", actor_id: "actor-maya", refs: ["card:b"] },
    ];
    expect(updateDigest(events, { actorName })).toBe("Maya updated 2 tasks");
    expect(updateDigest(events, { actorName, selfId: "actor-maya" })).toBe(
      "You updated 2 tasks",
    );
    expect(shortActorName("codex on m5-mbp")).toBe("codex on m5-mbp");
  });

  it("falls back to a count when no events came with the group", () => {
    expect(updateDigest([], { unreadCount: 4 })).toBe("4 new changes");
  });

  it("splits a phrase around its object so the object can be a link", () => {
    expect(
      eventPhraseParts({
        verb: "moved {object} to review",
        objectTitle: "Bug bash",
      }),
    ).toEqual({ before: "moved ", object: "Bug bash", after: " to review" });
  });
});

describe("inbox response queue", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetInboxResponseQueue();
    client.respondInboxItem.mockReset();
    client.respondInboxItem.mockResolvedValue({ event: { id: "e1" } });
  });
  afterEach(() => {
    resetInboxResponseQueue();
    vi.useRealTimers();
  });

  const request = {
    response_text: "Ship it",
    outcome: "answered",
    notify_mode: "original",
  };

  it("commits the exact request once the undo window closes", async () => {
    queueInboxResponse({ itemId: "inbox:a", request, message: "Sent to Omar" });
    expect(client.respondInboxItem).not.toHaveBeenCalled();
    expect(get(inboxResponseToast)).toMatchObject({ state: "pending" });
    expect(get(inboxResponseOverlay)["inbox:a"]).toMatchObject({
      status: "pending",
      outcome: "answered",
    });
    await vi.advanceTimersByTimeAsync(UNDO_WINDOW_MS);
    expect(client.respondInboxItem).toHaveBeenCalledTimes(1);
    expect(client.respondInboxItem).toHaveBeenCalledWith("inbox:a", {
      ...request,
      idempotency_key: expect.any(String),
    });
    expect(get(inboxResponseToast)).toMatchObject({ state: "sent" });
  });

  it("sends nothing when undone and hands back what was queued", async () => {
    queueInboxResponse({
      itemId: "inbox:a",
      request,
      message: "Sent",
      restore: { reply: "draft" },
    });
    const undone = undoInboxResponse();
    expect(undone).toMatchObject({
      itemId: "inbox:a",
      restore: { reply: "draft" },
    });
    await vi.advanceTimersByTimeAsync(UNDO_WINDOW_MS * 2);
    expect(client.respondInboxItem).not.toHaveBeenCalled();
    expect(get(inboxResponseOverlay)["inbox:a"]).toBeUndefined();
    expect(get(inboxResponseToast)).toBeNull();
  });

  it("commits the waiting response at once when another is queued", async () => {
    queueInboxResponse({ itemId: "inbox:a", request, message: "A" });
    queueInboxResponse({ itemId: "inbox:b", request, message: "B" });
    await vi.advanceTimersByTimeAsync(0);
    expect(client.respondInboxItem).toHaveBeenCalledWith("inbox:a", {
      ...request,
      idempotency_key: expect.any(String),
    });
    expect(undoInboxResponse()?.itemId).toBe("inbox:b");
  });

  it("puts a failed response back in front of the reader", async () => {
    client.respondInboxItem.mockRejectedValue(new Error("core unavailable"));
    queueInboxResponse({ itemId: "inbox:a", request, message: "A" });
    await flushInboxResponse();
    expect(get(inboxResponseToast)).toMatchObject({
      state: "failed",
      error: expect.stringContaining("core unavailable"),
    });
    expect(get(inboxResponseOverlay)["inbox:a"]).toBeUndefined();
  });

  it("retries an ambiguous failure with the same idempotency key", async () => {
    client.respondInboxItem.mockRejectedValueOnce(new Error("connection lost"));
    queueInboxResponse({ itemId: "inbox:a", request, message: "A" });
    await flushInboxResponse();
    const first = client.respondInboxItem.mock.calls[0][1];
    expect(first.idempotency_key).toMatch(/^[0-9a-f-]{36}$/i);
    await retryInboxResponse();
    const retry = client.respondInboxItem.mock.calls[1][1];
    expect(retry).toEqual(first);
  });

  it("files answered items as completed until core catches up", () => {
    const items = [
      { id: "inbox:a", status: "open" },
      { id: "inbox:b", status: "open" },
    ];
    const overlay = {
      "inbox:a": {
        status: "pending",
        response_text: "Ship it",
        outcome: "rejected",
        responded_at: "t",
      },
    };
    const [a, b] = applyResponseOverlay(items, overlay);
    expect(a).toMatchObject({
      status: "completed",
      response_text: "Ship it",
      outcome: "rejected",
    });
    expect(b.status).toBe("open");
  });

  it("notifies the requester unless core says it cannot reach them", () => {
    expect(defaultNotifyMode({})).toBe("original");
    expect(
      defaultNotifyMode({ notification_target_status: { resolvable: false } }),
    ).toBe("none");
  });
});

describe("inbox shortcuts", () => {
  it("maps triage keys", () => {
    expect(inboxShortcutAction(key("j"))).toEqual({ type: "next" });
    expect(inboxShortcutAction(key("k"))).toEqual({ type: "previous" });
    expect(inboxShortcutAction(key("3"))).toEqual({
      type: "proposal",
      index: 3,
    });
    expect(inboxShortcutAction(key("6"))).toBeNull();
    expect(inboxShortcutAction(key("r"))).toEqual({ type: "reply" });
    expect(inboxShortcutAction(key("e"))).toEqual({ type: "done" });
    expect(inboxShortcutAction(key("o"))).toEqual({ type: "open" });
    expect(inboxShortcutAction(key("?"))).toEqual({ type: "help" });
    expect(inboxShortcutAction(key("z", { metaKey: true }))).toEqual({
      type: "undo",
    });
  });

  it("stays out of the way while typing, with modifiers, or under a dialog", () => {
    const textarea = document.createElement("textarea");
    expect(isTextEntryTarget(textarea)).toBe(true);
    expect(inboxShortcutAction(key("j", { target: textarea }))).toBeNull();
    expect(inboxShortcutAction(key("1", { target: textarea }))).toBeNull();
    expect(inboxShortcutAction(key("j", { metaKey: true }))).toBeNull();
    expect(inboxShortcutAction(key("j", { altKey: true }))).toBeNull();
    expect(inboxShortcutAction(key("j"), { modalOpen: true })).toBeNull();
    expect(inboxShortcutAction(key("j"), { helpOpen: true })).toBeNull();
    expect(inboxShortcutAction(key("Escape"), { helpOpen: true })).toEqual({
      type: "close-help",
    });
    const checkbox = document.createElement("input");
    checkbox.setAttribute("type", "checkbox");
    expect(isTextEntryTarget(checkbox)).toBe(false);
  });
});

describe("inbox context", () => {
  it("reads the task's own thread first, then the item's threads", () => {
    expect(
      contextThreads(
        { related_refs: ["thread:project", "card:x"], thread_id: "ask-thread" },
        { kind: "card", ref: "card:lock-hub", work: { handle: "lock-hub" } },
      ),
    ).toEqual(["thread:lock-hub", "thread:project", "thread:ask-thread"]);
  });

  it("prefers the requester's own latest note over newer chatter", () => {
    const note = pickProgressNote(
      [
        {
          id: "m1",
          type: "message_posted",
          actor_id: "actor-omar",
          ts: "2026-09-27T09:00:00Z",
          payload: { text: "Both branches playable; need a default." },
        },
        {
          id: "m2",
          type: "message_posted",
          actor_id: "actor-qa",
          ts: "2026-09-27T10:00:00Z",
          payload: { text: "Unrelated" },
        },
      ],
      "actor-omar",
    );
    expect(note).toMatchObject({
      id: "m1",
      actorId: "actor-omar",
      byRequester: true,
      text: "Both branches playable; need a default.",
    });
    expect(pickProgressNote([], "actor-omar")).toBeNull();
  });
});

it("collapses repeated agent edits into distinct cards on the subject", () => {
  const events = ["a", "a", "b", "b", "b"].map((card, index) => ({
    id: String(index),
    type: "card_updated",
    actor_id: "claude",
    refs: [`card:${card}`],
  }));
  expect(
    updateDigest(events, {
      actorName: () => "claude",
      isAgent: () => true,
      groupRef: "board:omi",
      titleFor: () => "Omi · Initiatives",
    }),
  ).toBe("claude reorganized Omi · Initiatives: 2 cards");
});
