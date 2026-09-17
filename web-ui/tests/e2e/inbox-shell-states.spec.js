import { expect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

/**
 * Walks the workspace shell (sidebar, account menu, command palette, first-run
 * tour, actor gate, session overlays) and the Inbox surfaces (list, detail
 * pane, standalone item route, login) through every state they can reach, at
 * several viewport sizes, running the geometry audit after each transition.
 *
 * Fixture data is deliberately ugly: 64-char ids, long unbroken handles, long
 * error messages and enough rows to scroll.
 */

const WS = "/o/local/w/local";
const INBOX_PATH = `${WS}/inbox`;
const bothEnds = { scrollPositions: ["top", "bottom"] };
// The dev server compiles a route on first hit; first paint can lag under load.
const FIRST_PAINT = { timeout: 20_000 };

const LONG_HASH =
  "1fb951be68b4aa395611181d1d7af40857ea6c038e26393eab6433516f2ee888";
const SELF_AGENT_ID = `agent_${LONG_HASH}`;
const SELF_ACTOR_ID = `actor_${LONG_HASH.slice(0, 40)}`;
const LONG_EMAIL = `hermes.operations.escalations+${LONG_HASH.slice(0, 32)}@long-subdomain.example.com`;
const LONG_TITLE = `Escalation ${LONG_HASH} must be answered before the rollout window closes`;
const LONG_ERROR =
  "anx-core request failed at same-origin: GET /pm/decisions (503) - upstream workspace runtime is unreachable; retry after the core process finishes its schema handshake ";

function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/**
 * The core path a request targets, or "" when the URL is a SvelteKit route (a
 * page or its `__data.json`) rather than a core API call. Core is reached
 * either directly (`/inbox`) or through the hosted BFF (`/ws/{org}/{ws}/inbox`).
 */
function corePathOf(urlLike) {
  const url = typeof urlLike === "string" ? new URL(urlLike) : urlLike;
  let pathname = url.pathname;
  if (pathname.length > 1 && pathname.endsWith("/")) {
    pathname = pathname.slice(0, -1);
  }
  if (/^\/o\/[^/]+\/w\/[^/]+(\/|$)/.test(pathname)) return "";
  const hosted = pathname.match(/^\/ws\/[^/]+\/[^/]+(\/.*)?$/);
  if (hosted) pathname = hosted[1] || "/";
  return pathname;
}

/** @param {RegExp} pattern */
function core(pattern) {
  return (url) => {
    const pathname = corePathOf(url);
    return Boolean(pathname) && pattern.test(pathname);
  };
}

function hoursAgo(hours) {
  return new Date(Date.now() - hours * 60 * 60 * 1000).toISOString();
}

function inboxItem(overrides) {
  return {
    status: "open",
    kind: "ask",
    thread_id: "thread-onboarding",
    subject_ref: "thread:thread-onboarding",
    related_refs: ["thread:thread-onboarding"],
    response_proposals: [],
    requester_agent_id: SELF_AGENT_ID,
    source_event_time: hoursAgo(3),
    ...overrides,
  };
}

const OPEN_INBOX_ITEMS = [
  inboxItem({
    id: "inbox-long-ask",
    title: LONG_TITLE,
    body: `The upstream handshake keeps failing with ${LONG_HASH}. Full trace: ${LONG_HASH}${LONG_HASH}`,
    severity: "critical",
    requester_label: LONG_EMAIL,
    response_proposals: [
      "Yes — proceed with the exception and record the reason in the runbook.",
      `Hold until ${LONG_EMAIL} confirms the signer`,
      LONG_HASH,
    ],
    source_event_time: hoursAgo(1),
  }),
  inboxItem({
    id: "inbox-review",
    kind: "review",
    title: "Review updated runbook draft",
    body: "Please review the runbook draft before the window closes.",
    severity: "high",
    requester_label: "riley@example.com",
    response_proposals: ["Approved.", "Request revisions."],
    source_event_time: hoursAgo(2),
  }),
  inboxItem({
    id: "inbox-plain",
    kind: "escalate",
    title: "Missing legal signer",
    requester_label: "",
    requester_agent_id: SELF_AGENT_ID,
    source_event_time: hoursAgo(26),
  }),
  // Enough rows that the list scrolls at every viewport.
  ...Array.from({ length: 14 }, (_value, index) =>
    inboxItem({
      id: `inbox-bulk-${index}`,
      title: `Backlog item ${index} · ${LONG_HASH.slice(0, 24 + index)}`,
      requester_label: `agent-${LONG_HASH.slice(0, 18 + index)}`,
      source_event_time: hoursAgo(30 + index),
    }),
  ),
];

const COMPLETED_INBOX_ITEMS = [
  inboxItem({
    id: "completed:event-done-1",
    status: "completed",
    title: "Prior question resolved",
    response_text: `Ship it. Recorded against ${LONG_HASH}`,
    response_event_ref: "event:event-done-1",
    responded_at: hoursAgo(20),
    responding_actor_id: SELF_ACTOR_ID,
    original_request_missing: true,
  }),
];

const WORK = [
  {
    id: "card-blocked",
    ref: "card:card-blocked",
    handle: "card-blocked",
    title: `Blocked task with a very long name ${LONG_HASH.slice(0, 28)}`,
    phase: "blocked",
    blockers: [
      `Waiting on ${LONG_EMAIL}`,
      `Upstream ref ${LONG_HASH} never resolved`,
    ],
    next_actor: LONG_EMAIL,
    next_action: "Confirm the signer and re-run the handshake.",
    updated_at: hoursAgo(4),
    source: { authority: "github", revision: "rev-1" },
    freshness: { last_observed_at: hoursAgo(5) },
    refresh: {
      last_error: {
        message: `reader not ready: ${LONG_HASH}`,
        code: "reader_not_ready",
      },
    },
  },
  {
    id: "card-nexus",
    ref: "card:card-nexus",
    handle: "card-nexus",
    title: "Nexus-owned task",
    phase: "in_progress",
    decision_revision: "rev-9",
    updated_at: hoursAgo(6),
    source: { authority: "nexus" },
    freshness: { last_observed_at: hoursAgo(6) },
  },
];

const DECISIONS = [
  {
    id: `dec_${LONG_HASH}`,
    status: "awaiting_answer",
    scope: "work.phase",
    work_ref: "card:card-nexus",
    actor_id: SELF_ACTOR_ID,
    revision: 3,
    target_revision: "rev-9",
    payload: { phase: "done", resolution_refs: [`artifact:${LONG_HASH}`] },
    instruction: `Decision: move the task to done. Evidence ${LONG_HASH}`,
    created_at: hoursAgo(2),
    updated_at: hoursAgo(2),
  },
  {
    id: "dec_answered",
    status: "answered",
    scope: "work.phase",
    work_ref: "card:card-nexus",
    actor_id: SELF_ACTOR_ID,
    action_id: "act_answered",
    answer: `Approved because ${LONG_HASH}`,
    answered_by: SELF_ACTOR_ID,
    payload: { phase: "done" },
    target_revision: "rev-9",
    instruction: "Decision: move the task to done.",
    created_at: hoursAgo(8),
    updated_at: hoursAgo(8),
  },
  {
    id: "dec_superseded",
    status: "superseded",
    scope: "work.annotate",
    work_ref: "card:card-blocked",
    actor_id: SELF_ACTOR_ID,
    superseded_by: `dec_${LONG_HASH}`,
    superseded_reason: `replaced by a fresher proposal ${LONG_HASH}`,
    instruction: "Decision: annotate the task with the latest source reading.",
    created_at: hoursAgo(9),
    updated_at: hoursAgo(9),
  },
];

const ACTIONS = [
  {
    id: "act_answered",
    decision_id: "dec_answered",
    status: "failed",
    scope: "work.phase",
    target_revision: "rev-9",
    deliverable: true,
    authorization_basis: `decision:${LONG_HASH}`,
    attempts: [
      {
        status: "failed",
        sent_at: hoursAgo(7),
        detail: `refused ${LONG_HASH}`,
      },
    ],
    receipt: {
      detail: `The source refused the change at phase: in_progress. Correlation id ${LONG_HASH}`,
      external_id: LONG_HASH,
    },
  },
];

const UPDATES = [
  {
    group_ref: `topic:topic_${LONG_HASH.slice(0, 32)}`,
    group_type: "topic",
    display_name: `Topic with an extremely long display name ${LONG_HASH.slice(0, 30)}`,
    unread_count: 12,
    newest_event: {
      id: "evt-1",
      ts: hoursAgo(1),
      summary: `Newest event summary ${LONG_HASH}`,
    },
  },
];

/**
 * Mutable mock of every core endpoint the shell and the Inbox touch. Each
 * handler reads `api` at request time, so a test flips a field and drives the
 * UI:
 *   - `hold.<name>`: a deferred the response waits on (in-flight states)
 *   - `fail.<name>`: respond with an error body
 */
async function installWorkspaceApi(page, overrides = {}) {
  const api = {
    self: {
      agent_id: SELF_AGENT_ID,
      actor_id: SELF_ACTOR_ID,
      username: LONG_EMAIL,
    },
    authenticated: true,
    sessionStatus: 200,
    devActorMode: false,
    tourSeen: true,
    actors: [
      { id: SELF_ACTOR_ID, display_name: LONG_EMAIL, tags: ["human"] },
      {
        id: "actor-second",
        display_name: `Riley ${LONG_HASH.slice(0, 24)}`,
        tags: ["human"],
      },
    ],
    principals: [
      {
        agent_id: SELF_AGENT_ID,
        actor_id: SELF_ACTOR_ID,
        username: LONG_EMAIL,
        principal_kind: "human",
        auth_method: "passkey",
        revoked: false,
      },
      {
        agent_id: "agent-hermes",
        actor_id: "actor-hermes",
        username: "m4-hermes",
        principal_kind: "agent",
        auth_method: "public_key",
        revoked: false,
      },
    ],
    personas: [],
    decisions: DECISIONS,
    actions: ACTIONS,
    work: WORK,
    inboxOpen: OPEN_INBOX_ITEMS,
    inboxCompleted: COMPLETED_INBOX_ITEMS,
    updates: UPDATES,
    documents: [],
    truncated: false,
    hold: {},
    fail: {},
    calls: [],
    ...overrides,
  };

  const json = (route, status, body) =>
    route.fulfill({
      status,
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });
  const errorBody = (message, code = "test_failure") => ({
    error: { code, message, details: message },
  });

  async function respond(route, name, okBody) {
    api.calls.push(name);
    if (api.hold[name]) await api.hold[name].promise;
    if (api.fail[name]) {
      const failure = api.fail[name];
      return json(
        route,
        failure.status ?? 500,
        failure.body ?? errorBody(failure.message ?? String(failure)),
      );
    }
    return json(route, 200, typeof okBody === "function" ? okBody() : okBody);
  }

  await page.addInitScript(
    ({ seen, actorId }) => {
      if (seen) localStorage.setItem("workspaceTourSeen.local", "1");
      else localStorage.removeItem("workspaceTourSeen.local");
      if (actorId) localStorage.setItem("anx_ui_actor_id:local", actorId);
    },
    { seen: api.tourSeen, actorId: api.authenticated ? "" : "" },
  );

  await page.context().addCookies([
    {
      name: "anx_ui_session_local",
      value: "test-refresh-token",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);

  await page.route("**/auth/session", (route) => {
    if (api.sessionStatus !== 200) {
      return json(route, api.sessionStatus, {
        error: {
          code: "session_ended_by_account_status",
          message: "This account is no longer active.",
        },
      });
    }
    return json(
      route,
      200,
      api.authenticated
        ? { authenticated: true, agent: api.self }
        : { authenticated: false },
    );
  });

  await page.route(core(/^\/meta\/handshake$/), (route) =>
    json(route, 200, {
      schema_version: "0.6.0",
      command_registry_digest: "e2e",
      core_version: "test",
      api_version: "0.2",
      dev_actor_mode: api.devActorMode,
      human_auth_mode: "passkey",
    }),
  );

  await page.route(core(/^\/auth\/bootstrap\/status$/), (route) =>
    respond(route, "bootstrapStatus", () => ({
      dev_passkey_bypass_available: api.devActorMode,
      bootstrap_required: false,
    })),
  );

  await page.route(core(/^\/auth\/dev\/identities$/), (route) =>
    json(route, 200, { personas: api.personas }),
  );

  await page.route(core(/^\/auth\/dev\/session$/), (route) =>
    respond(route, "devSession", { ok: true }),
  );

  await page.route(core(/^\/actors$/), (route) => {
    if (route.request().method() === "POST") {
      return respond(route, "createActor", () => {
        const payload = route.request().postDataJSON() ?? {};
        const actor = {
          id: payload.id || "actor-new",
          display_name: payload.display_name || "New actor",
          tags: ["human"],
        };
        api.actors = [...api.actors, actor];
        return { actor };
      });
    }
    return respond(route, "actors", () => ({ actors: api.actors }));
  });

  await page.route(core(/^\/auth\/principals$/), (route) =>
    respond(route, "principals", () => ({
      principals: api.principals,
      active_human_principal_count: 1,
      next_cursor: "",
    })),
  );

  await page.route(core(/^\/pm\/decisions$/), (route) =>
    respond(route, "decisions", () => ({
      items: api.decisions,
      has_more: api.truncated === true,
    })),
  );

  await page.route(core(/^\/pm\/decisions\/[^/]+$/), (route) =>
    respond(route, "decision", () => {
      const id = decodeURIComponent(
        corePathOf(route.request().url()).split("/")[3],
      );
      return api.decisions.find((item) => item.id === id) ?? api.decisions[0];
    }),
  );

  await page.route(core(/^\/pm\/decisions\/[^/]+\/answer$/), (route) =>
    respond(route, "answer", () => {
      const id = decodeURIComponent(
        corePathOf(route.request().url()).split("/")[3],
      );
      const body = route.request().postDataJSON() ?? {};
      const answered = {
        ...(api.decisions.find((item) => item.id === id) ?? api.decisions[0]),
        status: body.approve ? "answered" : "declined",
        answer: body.text,
        answered_by: SELF_ACTOR_ID,
        action_id: body.approve ? "act_new" : undefined,
      };
      api.decisions = api.decisions.map((item) =>
        item.id === answered.id ? answered : item,
      );
      return answered;
    }),
  );

  await page.route(core(/^\/pm\/decisions\/[^/]+\/dispatch$/), (route) =>
    respond(route, "dispatch", () => ({
      id: "act_new",
      decision_id: `dec_${LONG_HASH}`,
      status: "pending_delivery",
      deliverable: true,
      attempts: [],
    })),
  );

  await page.route(core(/^\/pm\/actions$/), (route) =>
    respond(route, "actions", () => ({ items: api.actions, has_more: false })),
  );

  await page.route(core(/^\/pm\/actions\/[^/]+$/), (route) =>
    respond(route, "action", () => {
      const id = decodeURIComponent(
        corePathOf(route.request().url()).split("/")[3],
      );
      return api.actions.find((item) => item.id === id) ?? api.actions[0];
    }),
  );

  await page.route(
    core(/^\/pm\/actions\/[^/]+\/(reconcile|acknowledge)$/),
    (route) =>
      respond(route, "actionWrite", () => ({
        ...api.actions[0],
        status: "acknowledged",
      })),
  );

  await page.route(core(/^\/work$/), (route) =>
    respond(route, "work", () => ({ work: api.work, next_cursor: "" })),
  );

  await page.route(core(/^\/home\/unread$/), (route) =>
    respond(route, "unread", () => ({ groups: api.updates })),
  );
  await page.route(core(/^\/home\/read$/), (route) =>
    respond(route, "markRead", { ok: true }),
  );

  await page.route(core(/^\/inbox$/), (route) => {
    const status =
      new URL(route.request().url()).searchParams.get("status") || "open";
    const name = status === "completed" ? "inboxCompleted" : "inboxOpen";
    return respond(route, name, () => ({
      status,
      items: status === "completed" ? api.inboxCompleted : api.inboxOpen,
      // A cursor here means the page is showing a partial set: the mailbox
      // counts become lower bounds and the page must say so.
      next_cursor: api.truncated ? "cursor-2" : "",
      generated_at: new Date().toISOString(),
    }));
  });

  await page.route(core(/^\/inbox\/[^/]+$/), (route) =>
    respond(route, "inboxItem", () => {
      const id = decodeURIComponent(corePathOf(route.request().url()).slice(7));
      const found = [...api.inboxOpen, ...api.inboxCompleted].find(
        (item) => item.id === id,
      );
      return { item: found ?? null };
    }),
  );

  await page.route(core(/^\/inbox\/[^/]+\/respond$/), (route) =>
    respond(route, "respond", () => {
      const id = decodeURIComponent(
        corePathOf(route.request().url()).split("/")[2],
      );
      api.inboxOpen = api.inboxOpen.map((item) =>
        item.id === id
          ? {
              ...item,
              status: "completed",
              responded_at: new Date().toISOString(),
            }
          : item,
      );
      return {
        event: {
          id: "event-human-response",
          type: "human_attention_responded",
        },
        notify: { requested: true, queued: true, mode: "original" },
      };
    }),
  );

  await page.route(core(/^\/docs$/), (route) =>
    respond(route, "documents", () => ({ documents: api.documents })),
  );

  return api;
}

/**
 * The workspace catalog is server-rendered from `ANX_WORKSPACES`, which the e2e
 * server pins to a single workspace, so the sidebar picker never renders.
 * Rewrite the catalog in the SSR payload to exercise it.
 */
const SSR_ONE_WORKSPACE =
  'workspaces:[{organizationSlug:"local",slug:"local",label:"Local",description:""}]';
const SSR_MANY_WORKSPACES = `workspaces:[{organizationSlug:"local",slug:"local",label:"Local",description:""},{organizationSlug:"local",slug:"second",label:"Second workspace ${LONG_HASH.slice(0, 28)}",description:"A description long enough to need the ellipsis ${LONG_HASH.slice(0, 20)}"},{organizationSlug:"local",slug:"broken",label:"Unreachable workspace",description:"",_loadFailed:true}]`;

/** Only the workspace layout node carries `workspace:` next to `devActorMode:`. */
const SSR_WORKSPACE_NODE = "devActorMode:false,workspace:{";

/** Rewrites the SSR payload of workspace documents. */
async function patchSsrPayload(page, from, to) {
  await page.route(
    (url) => /^\/o\/[^/]+\/w\/[^/]+/.test(url.pathname),
    async (route) => {
      if (route.request().resourceType() !== "document") {
        return route.fallback();
      }
      const response = await route.fetch();
      const body = await response.text();
      if (!body.includes(from)) return route.fulfill({ response });
      return route.fulfill({ response, body: body.split(from).join(to) });
    },
  );
}

async function installMultipleWorkspaces(page) {
  await patchSsrPayload(page, SSR_ONE_WORKSPACE, SSR_MANY_WORKSPACES);
}

async function installCoreSchemaWarning(page, warning) {
  await patchSsrPayload(
    page,
    SSR_WORKSPACE_NODE,
    `devActorMode:false,coreSchemaCheckWarning:${JSON.stringify(warning)},workspace:{`,
  );
}

/** Below lg the detail pane replaces the list; return to it before picking. */
async function backToListIfNarrow(page) {
  const back = page.getByRole("link", { name: "← List" });
  if (await back.isVisible()) await back.click();
}

/** Signed in, Inbox loaded and painted. */
async function gotoInbox(page, search = "") {
  await page.goto(`${INBOX_PATH}${search}`);
  await expect(
    page.getByRole("heading", { name: "Inbox", exact: true }),
  ).toBeVisible(FIRST_PAINT);
}

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`inbox shell states @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
    });
    // Each test walks a dozen states and audits both scroll ends at each one;
    // that does not fit the default per-test budget on a shared dev server.
    test.beforeEach(() => {
      test.setTimeout(150_000);
    });

    test("inbox loading, populated, empty and failed", async ({ page }) => {
      const api = await installWorkspaceApi(page);
      api.hold.inboxOpen = deferred();
      api.hold.decisions = deferred();
      await page.goto(INBOX_PATH);
      await expect(
        page.locator('p[role="status"]', { hasText: "Loading inbox" }),
      ).toBeVisible(FIRST_PAINT);
      await expectCleanLayout(page, "inbox loading");

      api.hold.decisions.resolve();
      api.hold.inboxOpen.resolve();
      api.hold = {};
      await expect(page.getByTestId("inbox-row-inbox-long-ask")).toBeVisible();
      await expectCleanLayout(page, "inbox populated", bothEnds);

      for (const mailbox of ["Watching", "Handled", "Needs you"]) {
        await page.getByRole("link", { name: mailbox }).first().click();
        await expectCleanLayout(page, `mailbox ${mailbox}`, bothEnds);
      }

      // Refresh over populated data: the list must not jump or overlap.
      api.hold.inboxOpen = deferred();
      await page.getByRole("button", { name: "Reload" }).click();
      await expectCleanLayout(page, "inbox reloading over data");
      api.hold.inboxOpen.resolve();
      api.hold = {};
      await expect(page.getByRole("button", { name: "Reload" })).toBeEnabled();

      api.fail = {
        decisions: { message: LONG_ERROR.repeat(2) },
        inboxOpen: { message: "inbox projection unavailable" },
      };
      await page.getByRole("button", { name: "Reload" }).click();
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "inbox sections failed", bothEnds);

      api.fail = {};
      api.decisions = [];
      api.actions = [];
      api.work = [];
      api.inboxOpen = [];
      api.inboxCompleted = [];
      api.updates = [];
      await page.getByRole("button", { name: "Reload" }).click();
      await expect(page.getByText("You're clear.")).toBeVisible();
      await expectCleanLayout(page, "inbox empty", bothEnds);
      await page.getByRole("link", { name: "Watching" }).first().click();
      await expectCleanLayout(page, "watching empty", bothEnds);
      await page.getByRole("link", { name: "Handled" }).first().click();
      await expectCleanLayout(page, "handled empty", bothEnds);
    });

    test("inbox detail pane: item, task, update", async ({ page }) => {
      await installWorkspaceApi(page);
      await gotoInbox(page);

      await page.getByTestId("inbox-row-inbox-long-ask").click();
      await expect(
        page.getByRole("heading", { name: LONG_TITLE }),
      ).toBeVisible();
      await expectCleanLayout(page, "inbox item selected", bothEnds);

      await page.getByLabel("Reply").fill(LONG_HASH.repeat(2));
      await expectCleanLayout(page, "inbox reply drafted", bothEnds);
      await page.getByRole("button", { name: "Send reply" }).click();
      await expect(page.getByText("Response sent.")).toBeVisible();
      await expectCleanLayout(page, "inbox reply sent", bothEnds);

      // Below lg the pane replaces the list; go back before picking another.
      await backToListIfNarrow(page);
      const blocked = page
        .getByRole("link", { name: /Blocked task with a very long name/ })
        .first();
      await blocked.click();
      await expectCleanLayout(page, "task row selected", bothEnds);

      await backToListIfNarrow(page);
      await page.getByRole("link", { name: "Watching" }).first().click();
      const update = page
        .getByRole("link", { name: /Topic with an extremely/ })
        .first();
      await update.click();
      await expectCleanLayout(page, "update row selected", bothEnds);
      await page.getByRole("button", { name: "Mark read" }).click();
      await expectCleanLayout(page, "update marked read", bothEnds);
    });

    test("inbox decision panel and answer flow", async ({ page }) => {
      const api = await installWorkspaceApi(page);
      await gotoInbox(page);

      const decisionRow = page.locator(
        `[data-inbox-row="decision:dec_${LONG_HASH}"]`,
      );
      await decisionRow.click();
      await expect(page.getByRole("button", { name: "Approve" })).toBeVisible();
      await expectCleanLayout(page, "decision awaiting answer", bothEnds);

      await page.getByText("Technical details").click();
      await expectCleanLayout(page, "decision technical details", bothEnds);
      await page.getByText("Technical details").click();

      await page.getByLabel(/Your note/).fill(`Approved. ${LONG_HASH}`);
      await expectCleanLayout(page, "decision note filled", bothEnds);

      api.fail.answer = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Approve" }).click();
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "decision answer failed", bothEnds);

      api.fail = {};
      api.hold.answer = deferred();
      await page.getByRole("button", { name: "Approve" }).click();
      await expect(
        page.getByRole("button", { name: "Approving…" }),
      ).toBeVisible();
      await expectCleanLayout(page, "decision approving");
      api.hold.answer.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: "Approved" }),
      ).toBeVisible();
      await expectCleanLayout(page, "decision approved", bothEnds);

      // A failed receipt with a long detail and the attempts disclosure.
      await page.getByRole("link", { name: "Watching" }).first().click();
      await page.getByRole("link", { name: "Handled" }).first().click();
      await expectCleanLayout(page, "after answering", bothEnds);
    });

    test("inbox session expired", async ({ page }) => {
      const api = await installWorkspaceApi(page);
      api.fail.decisions = {
        status: 401,
        body: {
          error: {
            code: "invalid_token",
            message: "token is invalid, expired, or revoked",
          },
        },
      };
      api.fail.actions = api.fail.decisions;
      await page.goto(INBOX_PATH);
      await expect(
        page.getByRole("button", { name: "Sign in again" }),
      ).toBeVisible();
      await expectCleanLayout(page, "session expired banner", bothEnds);
    });

    test("shell chrome: sidebar, account menu, bottom nav", async ({
      page,
    }) => {
      await installWorkspaceApi(page);
      await gotoInbox(page);
      await expectCleanLayout(page, "shell at rest", bothEnds);

      const accountRow = page.getByRole("button", { name: "Account menu" });
      if (await accountRow.isVisible()) {
        await accountRow.click();
        await expect(page.getByRole("menu")).toBeVisible();
        await expectCleanLayout(page, "account menu open", bothEnds);
        await page.keyboard.press("Escape");
        await expect(page.getByRole("menu")).toHaveCount(0);
      } else {
        // Below lg the shell is the bottom nav; content must clear it.
        const bottomNav = page.getByRole("navigation", {
          name: "Primary navigation",
        });
        await expect(bottomNav).toBeVisible();
        await expectCleanLayout(page, "bottom nav over content", bothEnds);
        await page.getByTestId("inbox-row-inbox-plain").click();
        await expectCleanLayout(page, "detail pane over bottom nav", bothEnds);
        await page.getByRole("link", { name: "← List" }).click();
        await expectCleanLayout(page, "back to list", bothEnds);
      }
    });

    test("command palette states", async ({ page }) => {
      const api = await installWorkspaceApi(page, {
        documents: [
          {
            id: `doc_${LONG_HASH}`,
            title: `Runbook ${LONG_HASH}`,
            labels: ["ops"],
            updated_at: hoursAgo(2),
          },
        ],
      });
      await gotoInbox(page);

      await page.getByRole("button", { name: "Search workspace" }).click();
      const palette = page.getByRole("dialog", { name: "Command palette" });
      await expect(palette).toBeVisible();
      await expectCleanLayout(page, "palette empty");

      api.hold.documents = deferred();
      api.hold.work = deferred();
      await page.getByRole("combobox").fill(LONG_HASH);
      await expect(page.getByText("Searching...")).toBeVisible();
      await expectCleanLayout(page, "palette searching");
      api.hold.documents.resolve();
      api.hold.work.resolve();
      api.hold = {};
      await expect(page.getByText(`Runbook ${LONG_HASH}`)).toBeVisible();
      await expectCleanLayout(page, "palette results");

      api.documents = [];
      api.work = [];
      await page.getByRole("combobox").fill("zzz-nothing-matches");
      await expect(page.getByText("No results found")).toBeVisible();
      await expectCleanLayout(page, "palette empty results");
      await page.keyboard.press("Escape");
      await expect(palette).toHaveCount(0);
    });

    test("first-run onboarding tour", async ({ page }) => {
      await installWorkspaceApi(page, {
        tourSeen: false,
        principals: [
          {
            agent_id: SELF_AGENT_ID,
            actor_id: SELF_ACTOR_ID,
            username: LONG_EMAIL,
            principal_kind: "human",
            auth_method: "passkey",
            revoked: false,
          },
        ],
      });
      await page.goto(INBOX_PATH);
      const tour = page.getByTestId("workspace-spotlight-tour");
      await expect(tour).toBeVisible(FIRST_PAINT);
      await expectCleanLayout(page, "tour step 1 welcome");

      for (const step of ["1 of 5", "2 of 5", "3 of 5", "4 of 5", "5 of 5"]) {
        await page
          .getByRole("button", { name: /Take the tour|^Next$/ })
          .first()
          .click();
        await expect(tour.getByText(step, { exact: false })).toBeVisible();
        await expectCleanLayout(page, `tour ${step}`);
      }

      await page.getByRole("button", { name: "Close tour" }).click();
      await expect(tour).toHaveCount(0);
      await expectCleanLayout(page, "tour closed", bothEnds);
    });

    test("actor identity gate", async ({ page }) => {
      const api = await installWorkspaceApi(page, {
        authenticated: false,
        devActorMode: true,
      });
      api.hold.actors = deferred();
      await page.goto(INBOX_PATH);
      // The shell blocks on the bootstrap handshake before the gate can paint.
      await expect(page.getByText("Loading workspace…")).toBeVisible(
        FIRST_PAINT,
      );
      await expectCleanLayout(page, "workspace hydrating");

      api.hold.actors.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: "Select Actor Identity" }),
      ).toBeVisible();
      await expect(page.getByText(LONG_EMAIL).first()).toBeVisible();
      await expectCleanLayout(page, "actor gate populated", bothEnds);

      api.fail.createActor = { message: LONG_ERROR };
      await page.getByLabel("Display name").fill(LONG_HASH);
      await page.getByRole("button", { name: "Create and continue" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "actor gate create failed", bothEnds);
    });

    test("workspace login page", async ({ page }) => {
      const api = await installWorkspaceApi(page, {
        authenticated: false,
        devActorMode: true,
      });
      api.hold.bootstrapStatus = deferred();
      await page.goto(`${WS}/login`);
      await expect(page.getByLabel("Loading sign-in")).toBeVisible(FIRST_PAINT);
      await expectCleanLayout(page, "login loading");

      api.hold.bootstrapStatus.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: /Sign in/ }).first(),
      ).toBeVisible(FIRST_PAINT);
      await expectCleanLayout(page, "login form", bothEnds);

      await page.getByText("Local sign-in help").click();
      await page.getByText("Sign in with passkey", { exact: true }).click();
      await expectCleanLayout(page, "login details expanded", bothEnds);

      await page.getByLabel("Invite token").fill(LONG_HASH.repeat(2));
      await page.getByLabel("Display name", { exact: true }).fill(LONG_EMAIL);
      await expectCleanLayout(page, "login registration filled", bothEnds);

      // Passkey sign-in with no authenticator: the error is long and inline.
      await page
        .getByRole("button", { name: "Sign in with existing passkey" })
        .click();
      await expect(
        page.getByText(/passkey|WebAuthn|not supported/i).last(),
      ).toBeVisible();
      await expectCleanLayout(page, "login passkey error", bothEnds);
    });

    test("standalone inbox item route", async ({ page }) => {
      const api = await installWorkspaceApi(page);
      api.hold.inboxItem = deferred();
      await page.goto(`${WS}/inbox/inbox-review`);
      await expectCleanLayout(page, "item route loading");
      api.hold.inboxItem.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: "Review updated runbook draft" }),
      ).toBeVisible(FIRST_PAINT);
      await expectCleanLayout(page, "item route review", bothEnds);

      await page
        .getByRole("button", { name: /Request revisions|Approved\./ })
        .first()
        .click();
      await expectCleanLayout(page, "item route proposal applied", bothEnds);

      await page.getByRole("button", { name: "Someone else" }).click();
      await expectCleanLayout(page, "item route notify target", bothEnds);
      await page.getByPlaceholder("Search people or agents…").fill("actor");
      const options = page.getByRole("option");
      await expect(options.first()).toBeVisible();
      await expectCleanLayout(page, "item route actor menu open", bothEnds);
      // Escape must put the floating results away without clearing the form.
      await page.keyboard.press("Escape");
      await expect(options).toHaveCount(0);
      await expectCleanLayout(
        page,
        "item route actor menu dismissed",
        bothEnds,
      );
      await page.getByPlaceholder("Search people or agents…").fill("actor-");
      await expect(options.first()).toBeVisible();
      await options.first().click();
      await expectCleanLayout(page, "item route actor chosen", bothEnds);

      api.fail.respond = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Send response" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "item route submit failed", bothEnds);
    });

    test("standalone inbox item: completed and not found", async ({ page }) => {
      const api = await installWorkspaceApi(page);
      await page.goto(`${WS}/inbox/completed%3Aevent-done-1`);
      await expect(page.getByTestId("inbox-completed-detail")).toBeVisible(
        FIRST_PAINT,
      );
      await expectCleanLayout(page, "item route completed", bothEnds);

      api.fail.inboxItem = {
        status: 404,
        message: `No inbox item ${LONG_HASH} is open. ${LONG_ERROR}`,
      };
      await page.goto(`${WS}/inbox/${LONG_HASH}`);
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "item route not found", bothEnds);
    });

    test("workspace home redirect and session ended overlay", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page);
      api.hold.inboxOpen = deferred();
      await page.goto(WS);
      await expectCleanLayout(page, "workspace home redirecting");
      api.hold.inboxOpen.resolve();
      api.hold = {};
      await expect(page).toHaveURL(/\/inbox/);

      // A terminal account-status revocation paints a full-screen overlay.
      api.sessionStatus = 401;
      await page.goto(INBOX_PATH);
      await expect(
        page.getByRole("heading", { name: "Your session ended" }),
      ).toBeVisible(FIRST_PAINT);
      await expectCleanLayout(page, "session ended overlay", bothEnds);
    });

    test("inbox notices, deep links and partial loads", async ({ page }) => {
      const api = await installWorkspaceApi(page, { truncated: true });
      // A pinned row that is not in the loaded mailbox.
      await gotoInbox(page, "?item=decision:not-loaded");
      await expect(
        page.getByText("This item is not in the loaded mailbox."),
      ).toBeVisible();
      await expectCleanLayout(page, "pinned row missing", bothEnds);

      // has_more: counts become lower bounds and say so.
      await expect(
        page.getByText(
          "Not everything is loaded; the counts are lower bounds.",
        ),
      ).toBeVisible();
      await expectCleanLayout(page, "truncated counts", bothEnds);

      // A replaced proposal names its replacement.
      await page.goto(
        `${INBOX_PATH}?mailbox=watching&item=decision:dec_superseded`,
      );
      await expect(
        page.getByRole("link", { name: "Open the replacement" }),
      ).toBeVisible(FIRST_PAINT);
      await expectCleanLayout(page, "superseded decision", bothEnds);

      // A 409 that names a replacement adds a link under the error banner.
      await page.goto(`${INBOX_PATH}?item=decision:dec_${LONG_HASH}`);
      await page.getByLabel(/Your note/).fill("Approving this one.");
      api.fail.answer = {
        status: 409,
        body: {
          error: {
            code: "superseded",
            message: "superseded",
            details: { superseded_by: "dec_answered" },
          },
        },
      };
      await page.getByRole("button", { name: "Approve" }).click();
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "superseded on answer", bothEnds);

      // Dismissing an inbox item leaves a notice above the panes.
      api.fail = {};
      await page.goto(INBOX_PATH);
      await page.getByTestId("inbox-row-inbox-plain").click(FIRST_PAINT);
      await page.getByRole("button", { name: "Dismiss from Inbox" }).click();
      await expect(page.getByText("Dismissed from inbox only.")).toBeVisible();
      await expectCleanLayout(page, "dismissed notice", bothEnds);
    });

    test("dev persona switcher and workspace picker", async ({ page }) => {
      const api = await installWorkspaceApi(page, {
        devActorMode: true,
        personas: [
          {
            persona_id: "persona-human",
            actor_id: SELF_ACTOR_ID,
            principal_kind: "human",
            display_label: `Seeded human ${LONG_HASH.slice(0, 30)}`,
          },
          {
            persona_id: "persona-agent",
            actor_id: "actor-hermes",
            principal_kind: "agent",
            display_label: "m4-hermes",
          },
        ],
      });
      await installMultipleWorkspaces(page);
      await gotoInbox(page);

      const picker = page.locator(".workspace-switcher-trigger");
      if (await picker.isVisible()) {
        await picker.click();
        await expect(page.getByRole("listbox")).toBeVisible();
        await expectCleanLayout(page, "workspace picker open", bothEnds);
        await page.keyboard.press("Escape");
        await expect(page.getByRole("listbox")).toHaveCount(0);
      }

      const accountRow = page.getByRole("button", { name: "Account menu" });
      if (!(await accountRow.isVisible())) return;
      await accountRow.click();
      await page
        .getByRole("button", { name: "Switch fixture persona" })
        .click();
      await expectCleanLayout(page, "persona submenu open", bothEnds);

      api.fail.devSession = { status: 502, message: "seeded sign-in used up" };
      await page.getByRole("button", { name: /Seeded human/ }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "persona switch failed", bothEnds);
    });

    test("core schema warning banner over the inbox", async ({ page }) => {
      await installWorkspaceApi(page);
      await installCoreSchemaWarning(
        page,
        `anx-core handshake failed: digest mismatch ${LONG_HASH} (embedded registry is newer than the running core)`,
      );
      await page.goto(INBOX_PATH);
      await expect(page.getByText("Workspace core unreachable.")).toBeVisible(
        FIRST_PAINT,
      );
      await expectCleanLayout(page, "core schema warning", bothEnds);
    });

    test("decision outcome states: void, delivery and receipts", async ({
      page,
    }) => {
      const voidDecision = (id, flag) => ({
        id,
        status: "awaiting_answer",
        scope: "work.phase",
        work_ref: "card:card-nexus",
        actor_id: SELF_ACTOR_ID,
        target_revision: "rev-1",
        payload: { phase: "done" },
        instruction: `Decision: move the task to done. Ref ${LONG_HASH}`,
        created_at: hoursAgo(3),
        updated_at: hoursAgo(3),
        ...flag,
      });
      const api = await installWorkspaceApi(page, {
        decisions: [
          voidDecision("dec_stale", { target_current: false }),
          voidDecision("dec_moot", { already_at_target: true }),
          voidDecision("dec_gone", { work_missing: true }),
          {
            id: "dec_deliver",
            status: "answered",
            scope: "work.phase",
            work_ref: "card:card-nexus",
            actor_id: SELF_ACTOR_ID,
            action_id: "act_pending",
            answer: `Approved. ${LONG_HASH}`,
            answered_by: SELF_ACTOR_ID,
            payload: { phase: "done" },
            target_revision: "rev-9",
            instruction: "Decision: move the task to done.",
            created_at: hoursAgo(5),
            updated_at: hoursAgo(5),
          },
        ],
        actions: [
          {
            id: "act_pending",
            decision_id: "dec_deliver",
            status: "pending_delivery",
            scope: "work.phase",
            deliverable: true,
            target_revision: "rev-9",
            authorization_basis: `decision:${LONG_HASH}`,
            attempts: [],
          },
        ],
      });

      for (const id of ["dec_stale", "dec_moot", "dec_gone"]) {
        await page.goto(`${INBOX_PATH}?mailbox=watching&item=decision:${id}`);
        await expect(
          page.getByRole("button", { name: "Dismiss this proposal" }),
        ).toBeVisible(FIRST_PAINT);
        await expectCleanLayout(page, `void proposal ${id}`, bothEnds);
      }

      await page.goto(
        `${INBOX_PATH}?mailbox=watching&item=decision:dec_deliver`,
      );
      const deliver = page.getByRole("button", {
        name: "Deliver approved instruction",
      });
      await expect(deliver).toBeVisible(FIRST_PAINT);
      await expectCleanLayout(page, "approved awaiting delivery", bothEnds);

      api.hold.dispatch = deferred();
      await deliver.click();
      await expect(
        page.getByRole("button", { name: "Requesting delivery…" }),
      ).toBeVisible();
      await expectCleanLayout(page, "delivery in flight");
      api.hold.dispatch.resolve();
      api.hold = {};

      api.fail.dispatch = { message: LONG_ERROR };
      await page
        .getByRole("button", { name: "Deliver approved instruction" })
        .click();
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "delivery failed", bothEnds);

      // Receipts could not be read: the row says the delivery state is unknown.
      // The badge lives on the list row, which the pane replaces below lg.
      api.fail = { actions: { message: `receipts unavailable ${LONG_HASH}` } };
      await page.goto(INBOX_PATH);
      await expect(page.getByText("Delivery state unknown")).toBeVisible(
        FIRST_PAINT,
      );
      await expectCleanLayout(page, "receipts unavailable", bothEnds);
    });
  });
}
