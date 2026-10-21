import { describe, expect, it, vi } from "vitest";

import {
  isSafeAppPath,
  parseRouteRef,
  refRouteHref,
  resolveRefRoute,
} from "../../src/lib/refRoute.js";
import { buildLoginRedirectDestination } from "../../src/lib/workspaceBootstrap.js";
import { sanitizeReturnPath } from "../../src/lib/workspaceLaunchFlow.js";
import { workspacePath } from "../../src/lib/workspacePaths.js";

function httpError(status) {
  return Object.assign(new Error(`request failed (${status})`), { status });
}

/** A client whose every read answers from `records`, or 404s. */
function clientFor(records = {}) {
  const answer = (method, key) => {
    const value = records[method]?.[key];
    if (value instanceof Error) return Promise.reject(value);
    if (value === undefined) return Promise.reject(httpError(404));
    return Promise.resolve(value);
  };
  return {
    getWork: vi.fn((ref) => answer("getWork", ref)),
    getDocument: vi.fn((id) => answer("getDocument", id)),
    getTopic: vi.fn((id) => answer("getTopic", id)),
    getBoard: vi.fn((id) => answer("getBoard", id)),
    getThread: vi.fn((id) => answer("getThread", id)),
    getInboxItem: vi.fn((id) => answer("getInboxItem", id)),
    getEvent: vi.fn((id) => answer("getEvent", id)),
    getAgent: vi.fn((id) => answer("getAgent", id)),
    getArtifact: vi.fn((id) => answer("getArtifact", id)),
  };
}

describe("parseRouteRef", () => {
  it("accepts typed refs and maps aliases to their kind", () => {
    expect(parseRouteRef("card:launch-plan")).toMatchObject({
      ok: true,
      kind: "card",
      value: "launch-plan",
    });
    expect(parseRouteRef("doc:handbook").kind).toBe("document");
    expect(parseRouteRef("agent:builder").kind).toBe("actor");
    expect(
      parseRouteRef("inbox:human_attention:thread-1:none:evt-1"),
    ).toMatchObject({ ok: true, kind: "inbox" });
  });

  it("rejects malformed refs and unknown prefixes", () => {
    expect(parseRouteRef("")).toMatchObject({ ok: false, reason: "malformed" });
    expect(parseRouteRef("launch-plan").reason).toBe("malformed");
    expect(parseRouteRef("card:").reason).toBe("malformed");
    expect(parseRouteRef("Card:x").reason).toBe("malformed");
    expect(parseRouteRef("widget:x")).toMatchObject({
      ok: false,
      reason: "unknown_type",
      prefix: "widget",
    });
  });

  it("rejects values that could act as separators or escapes", () => {
    for (const raw of [
      "card:../..",
      "card:..",
      "card:.",
      "doc://evil.test",
      "doc:a/b",
      "doc:a\\b",
      "card:a%2Fb",
      "card:a?b",
      "card:a#b",
      "card:a b",
      "card:a\u0000b",
      "card:a\nb",
      `card:${"x".repeat(600)}`,
    ]) {
      expect(parseRouteRef(raw).ok, raw).toBe(false);
    }
  });
});

describe("resolveRefRoute", () => {
  it("opens a card by slug on its initiative page", async () => {
    const client = clientFor({
      getWork: {
        "card:launch-plan": {
          work: { id: "c-1", ref: "card:launch-plan", handle: "launch-plan" },
        },
      },
    });
    await expect(
      resolveRefRoute("card:launch-plan", client),
    ).resolves.toMatchObject({
      status: "redirect",
      path: "/initiatives/card%3Alaunch-plan",
    });
    expect(client.getWork).toHaveBeenCalledWith("card:launch-plan");
  });

  it("opens a card by id under its canonical handle", async () => {
    const client = clientFor({
      getWork: {
        "card:c-1": { work: { id: "c-1", handle: "launch-plan" } },
      },
    });
    const result = await resolveRefRoute("card:c-1", client);
    expect(result.path).toBe("/initiatives/card%3Alaunch-plan");
  });

  it("opens docs from either prefix", async () => {
    const client = clientFor({
      getDocument: {
        handbook: { document: { id: "d-1", handle: "handbook" } },
      },
    });
    for (const ref of ["doc:handbook", "document:handbook"]) {
      expect((await resolveRefRoute(ref, client)).path).toBe("/docs/handbook");
    }
  });

  it("sends a document revision to the revision page", async () => {
    expect(
      (await resolveRefRoute("document_revision:rev-1", clientFor())).path,
    ).toBe("/docs/revisions/rev-1");
  });

  it("opens a topic as the filtered initiatives list", async () => {
    const client = clientFor({
      getTopic: { roadmap: { topic: { id: "t-1", handle: "roadmap" } } },
    });
    expect((await resolveRefRoute("topic:roadmap", client)).path).toBe(
      "/initiatives?project_ref=topic%3Aroadmap",
    );
  });

  it("says a board has no page of its own", async () => {
    const client = clientFor({
      getBoard: { ops: { board: { id: "b-1", title: "Ops board" } } },
    });
    await expect(resolveRefRoute("board:ops", client)).resolves.toEqual({
      status: "no_page",
      ref: "board:ops",
      kind: "board",
      title: "Ops board",
    });
  });

  it("opens a thread", async () => {
    const client = clientFor({
      getThread: { "th-1": { thread: { id: "th-1" } } },
    });
    expect((await resolveRefRoute("thread:th-1", client)).path).toBe(
      "/threads/th-1",
    );
  });

  it("opens an inbox item as its decision", async () => {
    const id = "inbox:human_attention:th-1:none:evt-1";
    const client = clientFor({ getInboxItem: { [id]: { item: { id } } } });
    const result = await resolveRefRoute(id, client);
    expect(result.path).toBe(`/decide/${encodeURIComponent(id)}`);
    expect(client.getInboxItem).toHaveBeenCalledWith(id);
  });

  it("opens an ask (event ref from `anx ask`) as its decision", async () => {
    const client = clientFor({
      getEvent: {
        "ask-handle": {
          event: {
            id: "evt-9",
            type: "human_attention_requested",
            thread_id: "th-1",
          },
        },
      },
    });
    expect((await resolveRefRoute("event:ask-handle", client)).path).toBe(
      "/decide/event%3Aevt-9",
    );
  });

  it("opens other events in their thread", async () => {
    const client = clientFor({
      getEvent: {
        "evt-1": {
          event: { id: "evt-1", type: "message_posted", thread_id: "th-1" },
        },
        "evt-2": {
          event: { id: "evt-2", type: "card_moved", thread_id: "th-1" },
        },
        "evt-3": { event: { id: "evt-3", type: "card_moved" } },
      },
    });
    expect((await resolveRefRoute("event:evt-1", client)).path).toBe(
      "/threads/th-1?tab=messages#message-evt-1",
    );
    expect((await resolveRefRoute("event:evt-2", client)).path).toBe(
      "/threads/th-1?tab=timeline#event-evt-2",
    );
    expect((await resolveRefRoute("event:evt-3", client)).path).toBe(
      "/events#evt-3",
    );
  });

  it("opens actors and agents on the agent page", async () => {
    const client = clientFor({
      getAgent: {
        builder: { agent: { id: "agent-1", handle: "builder" } },
      },
    });
    for (const ref of ["actor:builder", "agent:builder"]) {
      expect((await resolveRefRoute(ref, client)).path).toBe("/agents/agent-1");
    }
  });

  it("opens an artifact through what it belongs to, or says it has no page", async () => {
    const client = clientFor({
      getArtifact: {
        "a-doc": { artifact: { id: "a-doc", owner_ref: "document:handbook" } },
        "a-file": {
          artifact: {
            id: "a-file",
            kind: "file",
            original_filename: "notes.txt",
          },
        },
      },
    });
    expect((await resolveRefRoute("artifact:a-doc", client)).path).toBe(
      "/docs/handbook",
    );
    expect(await resolveRefRoute("artifact:a-file", client)).toMatchObject({
      status: "no_page",
      title: "notes.txt",
    });
  });

  it("never follows a url ref", async () => {
    const client = clientFor();
    await expect(
      resolveRefRoute("url:https://evil.test/x", client),
    ).resolves.toMatchObject({ status: "no_page", kind: "url" });
    expect(Object.values(client).every((fn) => !fn.mock.calls.length)).toBe(
      true,
    );
  });

  it("reports unknown prefixes and malformed refs as not found without reading", async () => {
    const client = clientFor();
    await expect(resolveRefRoute("widget:x", client)).resolves.toMatchObject({
      status: "not_found",
      reason: "unknown_type",
    });
    await expect(resolveRefRoute("card:../..", client)).resolves.toMatchObject({
      status: "not_found",
      reason: "malformed",
    });
    expect(client.getWork).not.toHaveBeenCalled();
  });

  it("treats missing or hidden refs as not found", async () => {
    for (const status of [400, 403, 404]) {
      const client = clientFor({ getWork: { "card:x": httpError(status) } });
      await expect(resolveRefRoute("card:x", client)).resolves.toMatchObject({
        status: "not_found",
        ref: "card:x",
      });
    }
  });

  it("treats transient failures as retryable errors, not not-found", async () => {
    for (const failure of [
      httpError(503),
      httpError(401),
      new Error("offline"),
    ]) {
      const client = clientFor({ getDocument: { d: failure } });
      const result = await resolveRefRoute("doc:d", client);
      expect(result.status).toBe("error");
      expect(result.error).toBe(failure);
    }
  });

  it("does not let a record from core steer the path out of the workspace", async () => {
    const client = clientFor({
      getWork: {
        "card:x": { work: { handle: "../../evil", url: "https://evil.test" } },
      },
      getThread: { t: { thread: { id: "//evil.test" } } },
      getEvent: {
        e: {
          event: { id: "../x", type: "card_moved", thread_id: "//evil.test" },
        },
      },
    });
    expect((await resolveRefRoute("card:x", client)).path).toBe(
      "/initiatives/card%3Ax",
    );
    expect((await resolveRefRoute("thread:t", client)).path).toBe("/threads/t");
    expect((await resolveRefRoute("event:e", client)).path).toBe("/events#e");
  });
});

describe("refRouteHref", () => {
  it("confines a destination to the workspace", () => {
    expect(refRouteHref("acme", "main", "/initiatives/card%3Ax")).toBe(
      "/o/acme/w/main/initiatives/card%3Ax",
    );
    expect(
      refRouteHref("acme", "main", "/threads/t?tab=timeline#event-e", {
        origin: "https://app.example.test",
      }),
    ).toBe("/o/acme/w/main/threads/t?tab=timeline#event-e");
    expect(
      refRouteHref("acme", "main", "/decide/x", { basePath: "/anx" }),
    ).toBe("/anx/o/acme/w/main/decide/x");
  });

  it("refuses anything that leaves the origin or the workspace", () => {
    for (const path of [
      "https://evil.test/",
      "//evil.test/",
      "/\\evil.test",
      "/../../o/other/w/ws",
      "/docs/%2e%2e/%2e%2e",
      "javascript:alert(1)",
      "",
    ]) {
      expect(refRouteHref("acme", "main", path), path).toBe("");
      expect(isSafeAppPath(path), path).toBe(false);
    }
  });
});

describe("signing in from a ref link", () => {
  it("keeps /r/<ref> as the return path, raw or percent-encoded", () => {
    for (const path of ["/r/card:launch-plan", "/r/card%3Alaunch-plan"]) {
      expect(sanitizeReturnPath(path)).toBe(path);
      const destination = buildLoginRedirectDestination({
        hostedMode: false,
        organizationSlug: "acme",
        workspaceSlug: "main",
        currentAppPath: path,
        workspacePath,
      });
      const url = new URL(destination, "https://app.example.test");
      expect(url.pathname).toBe("/o/acme/w/main/login");
      expect(url.searchParams.get("return_to")).toBe(path);
    }
  });

  it("drops a traversal attempt back to the workspace home", () => {
    expect(sanitizeReturnPath("/r/card:..%2F..%2Fevil")).toBe("/");
  });
});
