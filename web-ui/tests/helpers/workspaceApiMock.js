/**
 * Mutable browser-side mock of the workspace API for layout-audit specs.
 *
 * Every endpoint reads its behavior from the returned `api` object at request
 * time, so a test flips a field and drives the UI into the next state:
 *   - `api.hold.<name>`: a deferred the response waits on (in-flight states)
 *   - `api.fail.<name>`: respond with an error body instead of the payload
 *
 * Only the endpoints the PM / threads / settings surfaces call are served;
 * anything else on the core origin answers 404 JSON so a page never blocks on
 * a real backend.
 */

import { expect } from "@playwright/test";

import { EXPECTED_SCHEMA_VERSION } from "../../src/lib/config.js";
import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";

/**
 * The workspace shell clips its main column (`overflow: hidden`), so content
 * wider than the viewport is cut off silently instead of making the document
 * scroll: the layout audit's `page-overflow-x` check cannot see it. This
 * asserts that no text is painted outside the viewport with no way to reach it.
 *
 * @param {import("@playwright/test").Page} page
 * @param {string} label
 */
export async function expectNoClippedContent(page, label) {
  const offenders = await page.evaluate(() => {
    const out = [];
    /**
     * How this element's overflow is cut off. A scrollable box can be brought
     * into view; a `truncate` / `line-clamp` box ends in an ellipsis, so the
     * reader can see there is more. Anything else just loses the text.
     */
    const clipper = (el) => {
      for (let node = el.parentElement; node; node = node.parentElement) {
        const style = getComputedStyle(node);
        if (style.overflowX === "visible") continue;
        return {
          node,
          scrollable:
            /(auto|scroll)/.test(style.overflowX) &&
            node.scrollWidth > node.clientWidth + 1,
          signposted:
            style.textOverflow === "ellipsis" ||
            style.getPropertyValue("-webkit-line-clamp") !== "none",
        };
      }
      return null;
    };
    for (const el of document.body.querySelectorAll("*")) {
      const own = Array.from(el.childNodes)
        .filter((node) => node.nodeType === 3)
        .map((node) => node.nodeValue)
        .join("")
        .trim();
      if (!own) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width < 3 || rect.height < 3) continue;
      if (rect.right <= innerWidth + 1 && rect.left >= -1) continue;
      if (rect.bottom <= 0 || rect.top >= innerHeight) continue;
      const style = getComputedStyle(el);
      if (style.visibility !== "visible" || style.position === "fixed")
        continue;
      if (parseFloat(style.opacity || "1") < 0.1) continue;
      const clip = clipper(el);
      if (clip?.scrollable || clip?.signposted) continue;
      out.push(
        `<${el.tagName.toLowerCase()}.${String(el.className).split(/\s+/).slice(0, 3).join(".")}> "${own.slice(0, 40)}" right=${Math.round(rect.right)} vw=${innerWidth}`,
      );
      if (out.length >= 6) break;
    }
    return out;
  });
  expect
    .soft(
      offenders,
      `Content is clipped outside the viewport in state "${label}":\n  ${offenders.join("\n  ")}`,
    )
    .toEqual([]);
}

export function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const SELF = {
  agent_id: "human-operator",
  actor_id: "actor-operator",
  username: "operator@example.com",
  principal_kind: "human",
  auth_method: "passkey",
};

/**
 * @param {import("@playwright/test").Page} page
 * @param {Record<string, unknown>} [overrides] seed fields merged into `api`
 */
export async function installWorkspaceApi(page, overrides = {}) {
  const digest = await getExpectedCommandRegistryDigest();
  const api = {
    self: SELF,
    authenticated: true,
    actors: [
      { id: "actor-operator", display_name: "Operator", tags: ["human"] },
    ],
    principals: [],
    conversations: [],
    conversationsHasMore: false,
    conversationsCursor: "",
    turns: [],
    turnsCursor: "",
    decisions: {},
    work: [],
    workCursor: "",
    capabilities: { refresh_executor_configured: true },
    secrets: [],
    revealValue: "example-secret-value-0000",
    threads: [],
    events: [],
    eventsPageInfo: { has_more: false, next_cursor: "" },
    timeline: [],
    topic: null,
    documents: [],
    artifacts: [],
    hold: {},
    fail: {},
    calls: [],
    ...overrides,
  };

  await page.addInitScript(() => {
    localStorage.setItem("workspaceTourSeen.local", "1");
    localStorage.setItem("workspaceTourSeen.local:local", "1");
  });
  await page.context().addCookies([
    {
      name: "anx_ui_session_local",
      value: "test-refresh-token",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = decodeURIComponent(url.pathname);
    const method = request.method();

    if (
      request.isNavigationRequest() ||
      path.startsWith("/o/") ||
      path.startsWith("/hosted/") ||
      path.startsWith("/@") ||
      path.startsWith("/src/") ||
      path.startsWith("/node_modules/") ||
      path.startsWith("/.svelte-kit/") ||
      path.startsWith("/_app/") ||
      path.includes("__data.json") ||
      /\.(js|css|svg|png|json|ico|woff2?)$/.test(path)
    ) {
      return route.continue();
    }

    const reply = (body, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    /** Gate + error hook shared by every business endpoint. */
    const respond = async (name, build, status = 200) => {
      api.calls.push({ name, path, method });
      if (api.hold[name]) await api.hold[name].promise;
      const failure = api.fail[name];
      if (failure) {
        return reply(
          failure.body ?? {
            error: {
              code: failure.code ?? "test_failure",
              message: failure.message ?? String(failure),
              details: failure.details ?? failure.message ?? String(failure),
            },
          },
          failure.status ?? 500,
        );
      }
      const body = typeof build === "function" ? build() : build;
      return reply(body, status);
    };

    if (path === "/meta/handshake" || path === "/version") {
      return reply({
        schema_version: EXPECTED_SCHEMA_VERSION,
        command_registry_digest: digest,
        api_version: "v1",
        core_version: "synthetic-ui-test",
        dev_actor_mode: false,
        human_auth_mode: "workspace_local",
      });
    }
    if (path === "/auth/session") {
      return reply(
        api.authenticated
          ? { authenticated: true, agent: api.self }
          : { authenticated: false },
      );
    }
    if (path === "/auth/bootstrap/status")
      return reply({ bootstrap_required: false });
    if (path === "/actors") return reply({ actors: api.actors });
    if (path === "/auth/principals")
      return reply({ principals: api.principals, next_cursor: "" });
    if (path === "/auth/invites") return reply({ invites: [] });
    if (path === "/auth/audit") return reply({ events: [], next_cursor: "" });
    if (path === "/home/unread")
      return reply({
        groups: [],
        unread_count: 0,
        group_count: 0,
        generated_at: new Date().toISOString(),
      });
    if (path === "/home/read") return reply({ ok: true });
    if (path === "/inbox") return reply({ items: [], total: 0 });
    if (path === "/boards") return reply({ boards: [] });
    if (path === "/topics") return reply({ topics: [] });
    if (path.startsWith("/stream/")) {
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: ": keepalive\n\n",
      });
    }

    // ---- Ask PM ------------------------------------------------------------
    if (path === "/pm/conversations" && method === "GET") {
      return respond("conversations", () => ({
        items: api.conversations,
        has_more: api.conversationsHasMore,
        next_cursor: api.conversationsCursor,
      }));
    }
    if (path === "/pm/conversations" && method === "POST") {
      return respond(
        "createConversation",
        () => {
          const body = request.postDataJSON() ?? {};
          const item = {
            id: `conversation-${api.conversations.length + 1}`,
            title: body.title,
            work_ref: body.work_ref,
            created_at: new Date().toISOString(),
          };
          api.conversations = [item, ...api.conversations];
          return item;
        },
        201,
      );
    }
    if (/^\/pm\/conversations\/[^/]+\/messages$/.test(path)) {
      return respond(
        "sendMessage",
        () => {
          const body = request.postDataJSON() ?? {};
          const turn = {
            id: `turn-${api.turns.length + 1}`,
            text: body.text,
            status: "queued",
            claimed: false,
            created_at: new Date().toISOString(),
          };
          api.turns = [...api.turns, turn];
          return turn;
        },
        202,
      );
    }
    if (/^\/pm\/conversations\/[^/]+$/.test(path)) {
      const cursor = url.searchParams.get("cursor") || "";
      return respond(cursor ? "olderTurns" : "conversation", () => ({
        conversation: api.conversations[0] ?? {
          id: "conversation-1",
          title: "",
        },
        turns: cursor ? api.olderTurns || [] : api.turns,
        next_cursor: cursor ? "" : api.turnsCursor,
      }));
    }
    if (/^\/pm\/decisions\/[^/]+$/.test(path)) {
      const id = path.split("/").pop();
      const record = api.decisions[id];
      if (!record) {
        api.calls.push({ name: `decision:${id}`, path, method });
        if (api.hold[`decision:${id}`])
          await api.hold[`decision:${id}`].promise;
        return reply({ error: { message: "decision not found" } }, 404);
      }
      return respond(`decision:${id}`, () => record);
    }
    if (path === "/pm/decisions") return reply({ items: [], has_more: false });
    if (path === "/pm/actions") return reply({ items: [], has_more: false });

    // ---- Work / integrations -----------------------------------------------
    if (path === "/work/capabilities")
      return respond("capabilities", () => ({
        capabilities: api.capabilities,
      }));
    if (path === "/work" && method === "GET")
      return respond("work", () => ({
        work: api.work,
        next_cursor: url.searchParams.get("cursor") ? "" : api.workCursor,
      }));

    // ---- Secrets -----------------------------------------------------------
    if (path === "/secrets" && method === "GET")
      return respond("secrets", () => ({ secrets: api.secrets }));
    if (path === "/secrets" && method === "POST")
      return respond(
        "createSecret",
        () => {
          const body = request.postDataJSON() ?? {};
          const secret = {
            id: `secret-${api.secrets.length + 1}`,
            name: body.name,
            description: body.description ?? "",
            updated_at: new Date().toISOString(),
          };
          api.secrets = [...api.secrets, secret];
          return { secret };
        },
        201,
      );
    if (/^\/secrets\/[^/]+\/reveal$/.test(path))
      return respond("revealSecret", () => ({ value: api.revealValue }));
    if (/^\/secrets\/[^/]+$/.test(path) && method === "DELETE")
      return respond("deleteSecret", () => {
        const id = path.split("/").pop();
        api.secrets = api.secrets.filter((secret) => secret.id !== id);
        return { ok: true };
      });

    // ---- Threads / topics --------------------------------------------------
    if (path === "/threads" && method === "GET")
      return respond("threads", () => ({ threads: api.threads }));
    if (/^\/(threads|topics)\/[^/]+\/timeline$/.test(path))
      return respond("timeline", () => ({
        events: api.timeline,
        artifacts: {},
        topics: {},
        cards: {},
        documents: {},
        document_revisions: {},
      }));
    if (/^\/(threads|topics)\/[^/]+\/workspace$/.test(path))
      return respond("threadWorkspace", () => ({
        thread_id: api.topic?.id ?? "",
        thread: api.topic,
        topic: api.topic,
        context: {
          recent_events: api.timeline,
          key_artifacts: [],
          open_cards: [],
          documents: api.documents,
        },
      }));
    if (/^\/(threads|topics)\/[^/]+$/.test(path) && method === "GET")
      return respond("thread", () => ({
        thread: api.topic,
        topic: api.topic,
      }));

    // ---- Docs / artifacts / events -----------------------------------------
    if (path === "/docs") return reply({ documents: api.documents });
    if (path === "/artifacts/attachments" && method === "POST")
      return respond("attach", () => {
        const artifact = {
          id: api.attachmentId ?? "artifact-upload-1",
          kind: "file",
          summary: api.attachmentName ?? "attachment.txt",
          original_filename: api.attachmentName ?? "attachment.txt",
          media_type: "text/plain",
          size_bytes: 12,
          created_at: new Date().toISOString(),
        };
        api.artifacts = [...api.artifacts, artifact];
        return { artifact };
      });
    if (path === "/artifacts") return reply({ artifacts: api.artifacts });
    if (path === "/events" && method === "GET")
      return respond("events", () => ({
        events: api.events,
        page_info: api.eventsPageInfo,
      }));
    if (path === "/events" && method === "POST")
      return respond("postEvent", () => {
        const body = request.postDataJSON() ?? {};
        const created = {
          id: `event-new-${api.timeline.length + 1}`,
          ts: new Date().toISOString(),
          actor_id: body.actor_id ?? api.self.actor_id,
          ...body.event,
        };
        api.timeline = [...api.timeline, created];
        return { event: created };
      });

    return reply({ error: { message: `Unmocked route ${path}` } }, 404);
  });

  return api;
}
