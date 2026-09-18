import { expect as baseExpect, test } from "@playwright/test";

/**
 * These specs drive four viewports against a shared dev server, so first paint
 * after a cold navigation can be slow; give assertions room instead of racing.
 */
const expect = baseExpect.configure({ timeout: 20_000 });

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

/**
 * Walks the Docs surfaces (list, detail, discussion rail, revision history,
 * settings, revision resolver) through every UI state they can reach and runs
 * the geometry audit after each transition at several viewport sizes.
 *
 * Fixture data is deliberately ugly: 64-char ids, unbroken tokens, long URLs,
 * multi-line error messages and wide markdown tables — the shapes that break
 * flex rows and floating layers.
 */

const DOCS_PATH = "/o/local/w/local/docs";
const LONG_HASH =
  "1fb951be68b4aa395611181d1d7af40857ea6c038e26393eab6433516f2ee888";
const ACTOR_ID = `actor_${LONG_HASH}`;
const LONG_TOKEN = `deploy-${"0123456789abcdef".repeat(5)}`;
const LONG_URL = `https://runbooks.internal.example.com/documents/${LONG_HASH}?trace=${LONG_HASH}`;
const LONG_SUMMARY =
  "Operator-facing summary that keeps going well past one line so the list row has to decide what to do with it. ".repeat(
    3,
  );
const LONG_TITLE = `Incident ${LONG_TOKEN} retrospective`;

const bothEnds = { scrollPositions: ["top", "bottom"] };
const DOC_BODY = [
  "# Launch readiness",
  "",
  "Check the OAuth callback copy before the public beta switch flips. The",
  "operator handoff note lives in the runbook below.",
  "",
  `Correlation id: ${LONG_TOKEN}${LONG_HASH}`,
  "",
  `Runbook: ${LONG_URL}`,
  "",
  "## Rollback",
  "",
  "| Step | Owner | Command | Window | Verification | Notes |",
  "| --- | --- | --- | --- | --- | --- |",
  `| Freeze writes | platform-oncall | \`anx docs get ${LONG_HASH}\` | 5m | dashboards flat | escalate to the launch war room |`,
  `| Drain queue | billing-oncall | \`anx work drain --queue ${LONG_TOKEN}\` | 12m | queue depth zero | keep the incident channel updated |`,
  "",
  "### Verification",
  "",
  "```",
  `curl -sS "${LONG_URL}" --header "x-anx-trace: ${LONG_HASH}" | jq .`,
  "```",
  "",
  "## Support handoff",
  "",
  "Keep this wording exact for support handoff and incident review.",
  "",
  "### Escalation",
  "",
  "- Page the launch war room.",
  `- Quote ${LONG_TOKEN} in the ticket.`,
].join("\n");

function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function doc(overrides) {
  const id = String(overrides?.id ?? "");
  return {
    // A public handle enables the CLI-copy affordance and the share menu.
    handle: id,
    ref: id ? `document:${id}` : "",
    state: "active",
    head_revision_number: 3,
    updated_at: "2026-03-28T10:00:00Z",
    updated_by: ACTOR_ID,
    created_at: "2026-03-01T10:00:00Z",
    created_by: ACTOR_ID,
    ...overrides,
  };
}

function revision(documentId, overrides = {}) {
  return {
    revision_id: `rev-${documentId}-3`,
    document_id: documentId,
    revision_number: 3,
    content_type: "text",
    content_hash: `sha256-${LONG_HASH}`,
    revision_hash: `revhash-${LONG_HASH}`,
    created_at: "2026-03-28T10:00:00Z",
    created_by: ACTOR_ID,
    content: DOC_BODY,
    ...overrides,
  };
}

/** Rows that stress the list: long strings, mixed lifecycle, noisy metadata. */
function listFixture() {
  const rows = [
    doc({
      id: "doc-launch-checklist",
      title: LONG_TITLE,
      summary: LONG_SUMMARY,
      source: LONG_URL,
      tags: ["knowledge", "launch", LONG_TOKEN],
      thread_id: "thread-launch-war-room",
      last_comment: {
        body: `Blocked on ${LONG_TOKEN} — see ${LONG_URL} before the next cut.`,
        created_at: "2026-03-28T09:00:00Z",
        created_by: ACTOR_ID,
      },
      comment_count: 12,
      head_revision_number: 17,
    }),
    doc({
      id: "doc-billing-runbook",
      title: "Billing runbook",
      summary: "How billing reconciles a failed checkout.",
      source: "ops/billing/runbook.md",
      tags: ["knowledge"],
      state: "archived",
      archived_at: "2026-03-20T10:00:00Z",
      archived_by: ACTOR_ID,
      last_comment: {
        body: "Update on Billing runbook",
        created_at: "2026-03-21T09:00:00Z",
      },
    }),
    doc({
      id: "doc-trashed-notes",
      title: LONG_TOKEN,
      summary: "",
      state: "trashed",
      trashed_at: "2026-03-22T10:00:00Z",
      trashed_by: ACTOR_ID,
    }),
    doc({
      id: "doc-plain-notes",
      title: "Meeting notes",
      summary: "",
    }),
  ];
  for (let i = 0; i < 18; i += 1) {
    rows.push(
      doc({
        id: `doc-bulk-${i}`,
        title: `Weekly operations review ${i}`,
        summary: i % 2 ? LONG_SUMMARY : "Short summary.",
        source: i % 3 ? LONG_URL : "",
        tags: i % 4 ? ["knowledge"] : [],
        head_revision_number: i + 1,
      }),
    );
  }
  return rows;
}

const DETAIL_DOCS = {
  "doc-launch-checklist": doc({
    id: "doc-launch-checklist",
    title: LONG_TITLE,
    summary: LONG_SUMMARY,
    source: LONG_URL,
    tags: ["knowledge", "launch", LONG_TOKEN],
    thread_id: "thread-launch-war-room",
    subject_ref: "topic:topic-launch",
    head_revision_id: "rev-doc-launch-checklist-3",
    head_revision_number: 3,
  }),
  "doc-plain-notes": doc({
    id: "doc-plain-notes",
    title: "Meeting notes",
    head_revision_id: "rev-doc-plain-notes-3",
  }),
  "doc-billing-runbook": doc({
    id: "doc-billing-runbook",
    title: "Billing runbook",
    summary: "How billing reconciles a failed checkout.",
    state: "archived",
    archived_at: "2026-03-20T10:00:00Z",
    archived_by: ACTOR_ID,
    head_revision_id: "rev-doc-billing-runbook-3",
  }),
  "doc-trashed-notes": doc({
    id: "doc-trashed-notes",
    title: LONG_TOKEN,
    state: "trashed",
    trashed_at: "2026-03-22T10:00:00Z",
    trashed_by: ACTOR_ID,
    trash_reason:
      `Superseded by ${LONG_URL} after the ${LONG_TOKEN} rollback. `.repeat(2),
    head_revision_id: "rev-doc-trashed-notes-3",
  }),
  "doc-structured": doc({
    id: "doc-structured",
    title: "Structured export",
    head_revision_id: "rev-doc-structured-3",
  }),
};

const DETAIL_REVISIONS = {
  "doc-launch-checklist": revision("doc-launch-checklist"),
  "doc-plain-notes": revision("doc-plain-notes", {
    content: "# Meeting notes\n\nShort body with nothing special in it.",
  }),
  "doc-billing-runbook": revision("doc-billing-runbook"),
  "doc-trashed-notes": revision("doc-trashed-notes"),
  "doc-structured": revision("doc-structured", {
    content_type: "application/json",
    content: '{\n  "rows": []\n}',
  }),
};

/** Older revisions, newest last (the page reverses them). */
function historyFor(documentId) {
  const head = DETAIL_REVISIONS[documentId];
  return [
    {
      ...head,
      revision_id: `rev-${documentId}-1`,
      revision_number: 1,
      created_at: "2026-03-01T10:00:00Z",
      revision_hash: `revhash-1-${LONG_HASH}`,
      content: "# First cut\n\nThe very first draft of this document.",
    },
    {
      ...head,
      revision_id: `rev-${documentId}-2`,
      revision_number: 2,
      created_at: "2026-03-14T10:00:00Z",
      revision_hash: `revhash-2-${LONG_HASH}`,
      content: `# Second cut\n\nStill rough. Tracking ${LONG_TOKEN}.`,
    },
    head,
  ];
}

const TIMELINE_EVENTS = [
  {
    id: "evt-doc-note-1",
    ts: "2026-03-28T08:00:00Z",
    type: "message_posted",
    actor_id: ACTOR_ID,
    thread_id: "thread-launch-war-room",
    refs: ["thread:thread-launch-war-room", "document:doc-launch-checklist"],
    summary: "Message: the readiness wording is close.",
    payload: {
      text: `The readiness wording is close. Blocked on ${LONG_TOKEN} — see ${LONG_URL}.`,
    },
  },
  {
    id: "evt-doc-note-2",
    ts: "2026-03-28T09:00:00Z",
    type: "message_posted",
    actor_id: "actor-second-human@example.com",
    thread_id: "thread-launch-war-room",
    refs: ["thread:thread-launch-war-room", "document:doc-launch-checklist"],
    summary: "Message: keep support handoff visible.",
    payload: { text: "Keep support handoff visible in the intro." },
  },
  {
    id: "evt-doc-anchor-1",
    ts: "2026-03-28T09:30:00Z",
    type: "message_posted",
    actor_id: ACTOR_ID,
    thread_id: "thread-launch-war-room",
    refs: [
      "thread:thread-launch-war-room",
      "document:doc-launch-checklist",
      "document_revision:rev-doc-launch-checklist-3",
    ],
    summary: "Message: this line needs one more pass.",
    payload: {
      kind: "document_text_comment",
      text: "This is the line that needs one more pass.",
      document_comment: {
        document_id: "doc-launch-checklist",
        revision_id: "rev-doc-launch-checklist-3",
        content_hash: `sha256-${LONG_HASH}`,
        selected_text: "Check the OAuth callback copy",
        context_before: "# Launch readiness\n\n",
        context_after: " before the public beta switch flips.",
        start_offset: DOC_BODY.indexOf("Check the OAuth callback copy"),
        end_offset:
          DOC_BODY.indexOf("Check the OAuth callback copy") +
          "Check the OAuth callback copy".length,
        anchor_status: "current",
      },
    },
  },
];

/**
 * Installs a mutable mock of every endpoint the docs routes call. Each handler
 * reads its behavior from `api` at request time, so a test flips a field and
 * then drives the UI.
 *   - `hold.<name>`: a deferred the response waits on (in-flight states)
 *   - `fail.<name>`: respond with an error body
 */
async function installDocsApi(page, overrides = {}) {
  const api = {
    documents: listFixture(),
    searchResults: null,
    searchNextCursor: "",
    detail: { ...DETAIL_DOCS },
    revisions: { ...DETAIL_REVISIONS },
    history: null,
    timeline: TIMELINE_EVENTS,
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
  const errorBody = (message) => ({
    error: { code: "test_failure", message, details: message },
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

  /**
   * Registers an API handler. The core API lives at the origin root, while page
   * routes (`/o/local/w/local/docs/…`) and Vite module URLs
   * (`/src/routes/…/docs/+page.svelte`) share the same path shapes — so match
   * the pathname anchored, never as a substring.
   */
  const apiRoute = (pathPattern, handler) =>
    page.route(
      (url) => pathPattern.test(url.pathname),
      (route) => handler(route, new URL(route.request().url()).pathname),
    );

  await page.addInitScript((actorId) => {
    localStorage.setItem("workspaceTourSeen.local", "1");
    localStorage.setItem("anx_ui_actor_id:local", actorId);
  }, ACTOR_ID);

  await apiRoute(/^\/actors$/, (route) =>
    json(route, 200, {
      actors: [
        { id: ACTOR_ID, display_name: "Operator", tags: ["human"] },
        {
          id: "actor-second-human@example.com",
          display_name: "actor-second-human@example.com",
          tags: ["human"],
        },
      ],
    }),
  );

  // Live updates: hold the SSE connection open instead of hammering retries.
  await apiRoute(
    /^\/stream\/(events|agent-notification-receipts)$/,
    () => new Promise(() => {}),
  );

  await apiRoute(/^\/docs$/, (route) => {
    if (route.request().method() === "POST") {
      return respond(route, "createDocument", () => {
        const body = route.request().postDataJSON() ?? {};
        const created = doc({
          id: "doc-created",
          title: body?.document?.title ?? "Created",
          summary: body?.document?.summary ?? "",
          head_revision_id: "rev-doc-created-3",
        });
        api.detail["doc-created"] = created;
        api.revisions["doc-created"] = revision("doc-created", {
          content: body?.content ?? "",
        });
        api.documents = [created, ...api.documents];
        return { document: created, revision: api.revisions["doc-created"] };
      });
    }
    return respond(route, "listDocuments", () => ({
      documents: api.documents,
    }));
  });

  await apiRoute(/^\/docs\/[^/]+$/, (route, pathname) => {
    const id = decodeURIComponent(pathname.split("/docs/")[1]);
    if (route.request().method() === "PATCH") {
      return respond(route, "patchDocument", () => {
        const patch = route.request().postDataJSON()?.patch ?? {};
        api.detail[id] = { ...api.detail[id], ...patch };
        return {
          document: api.detail[id],
          revision: api.revisions[id] ?? null,
        };
      });
    }
    return respond(route, "getDocument", () => {
      const document = api.detail[id];
      if (!document) return { document: null };
      return { document, revision: api.revisions[id] ?? null };
    });
  });

  await apiRoute(/^\/docs\/[^/]+\/revisions$/, (route, pathname) => {
    const id = decodeURIComponent(
      pathname.split("/docs/")[1].split("/revisions")[0],
    );
    if (route.request().method() === "POST") {
      return respond(route, "updateDocument", () => {
        const body = route.request().postDataJSON() ?? {};
        const next = {
          ...(api.revisions[id] ?? revision(id)),
          revision_id: `rev-${id}-4`,
          revision_number: 4,
          content: body.content ?? "",
          created_at: "2026-03-29T10:00:00Z",
        };
        api.revisions[id] = next;
        api.detail[id] = {
          ...api.detail[id],
          ...(body.document ?? {}),
          head_revision_id: next.revision_id,
          head_revision_number: 4,
        };
        return { document: api.detail[id], revision: next };
      });
    }
    return respond(route, "getDocumentHistory", () => ({
      revisions: api.history ?? historyFor(id),
    }));
  });

  await apiRoute(/^\/docs\/[^/]+\/revisions\/[^/]+$/, (route, pathname) => {
    const [id, revisionId] = pathname
      .split("/docs/")[1]
      .split("/revisions/")
      .map((part) => decodeURIComponent(part));
    return respond(route, "getDocumentRevision", () => ({
      revision:
        (api.history ?? historyFor(id)).find(
          (rev) => rev.revision_id === revisionId,
        ) ?? null,
    }));
  });

  await apiRoute(
    /^\/docs\/[^/]+\/(archive|unarchive|trash|restore)$/,
    (route, pathname) => {
      const [id, action] = pathname.split("/docs/")[1].split("/");
      return respond(route, `${action}Document`, () => {
        const current = api.detail[decodeURIComponent(id)];
        if (current) {
          const next = { ...current };
          if (action === "archive") {
            next.state = "archived";
            next.archived_at = "2026-03-29T10:00:00Z";
            next.archived_by = ACTOR_ID;
          } else if (action === "unarchive") {
            next.state = "active";
            next.archived_at = "";
            next.archived_by = "";
          } else if (action === "trash") {
            next.state = "trashed";
            next.trashed_at = "2026-03-29T10:00:00Z";
          } else {
            next.state = "active";
            next.trashed_at = "";
            next.trash_reason = "";
          }
          api.detail[decodeURIComponent(id)] = next;
        }
        api.documents = api.documents.map((row) =>
          row.id === decodeURIComponent(id)
            ? { ...row, state: action === "unarchive" ? "active" : row.state }
            : row,
        );
        return { ok: true };
      });
    },
  );

  // Registered after `/docs/{id}` so the more specific search route wins.
  await apiRoute(/^\/docs\/search$/, (route, pathname) => {
    void pathname;
    return respond(route, "searchDocuments", () => ({
      documents: api.searchResults ?? api.documents.slice(0, 6),
      next_cursor: api.searchNextCursor,
    }));
  });

  await apiRoute(/^\/threads\/[^/]+\/timeline$/, (route) =>
    respond(route, "threadTimeline", () => ({
      thread: { id: "thread-launch-war-room", title: LONG_TITLE },
      events: api.timeline,
      artifacts: {},
      topics: {},
      cards: {},
      documents: { "doc-launch-checklist": api.detail["doc-launch-checklist"] },
      document_revisions: {
        "rev-doc-launch-checklist-3": api.revisions["doc-launch-checklist"],
      },
    })),
  );

  await apiRoute(/^\/topics\/[^/]+$/, (route, pathname) =>
    respond(route, "getTopic", () => ({
      topic: {
        id: decodeURIComponent(pathname.split("/topics/")[1]),
        title: `Launch war room ${LONG_TOKEN}`,
      },
    })),
  );

  return api;
}

function docsUrl(suffix = "") {
  return `${DOCS_PATH}${suffix}`;
}

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`docs states @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      permissions: ["clipboard-read", "clipboard-write"],
    });
    // Each test walks a dozen states, each with a settle + audit pass.
    test.describe.configure({ timeout: 180_000 });

    test("list: loading, populated, empty and failed", async ({ page }) => {
      const api = await installDocsApi(page);
      api.hold.listDocuments = deferred();
      await page.goto(docsUrl());
      await expect(
        page.getByRole("heading", { name: "Docs", exact: true }),
      ).toBeVisible();
      await expectCleanLayout(page, "list loading");

      api.hold.listDocuments.resolve();
      api.hold = {};
      await expect(page.getByText("Billing runbook")).toBeVisible();
      await expectCleanLayout(page, "list populated", bothEnds);

      // Hovering a row swaps its timestamp for the quick-action overlay.
      const firstRow = page.locator("div.group\\/row").first();
      await firstRow.hover();
      await expectCleanLayout(page, "row quick actions");

      await firstRow.click({ button: "right" });
      await expect(
        page.getByRole("menuitem", { name: "Copy link" }),
      ).toBeVisible();
      await expectCleanLayout(page, "row context menu");
      await page.keyboard.press("Escape");

      api.fail.listDocuments = {
        message: `document index unavailable for ${LONG_TOKEN} `.repeat(4),
      };
      await page.reload();
      await expect(page.getByText(/document index unavailable/)).toBeVisible();
      await expectCleanLayout(page, "list failed", bothEnds);

      api.hold.listDocuments = deferred();
      await page.getByRole("button", { name: /Retry|Retrying/ }).click();
      await expectCleanLayout(page, "list retrying");
      api.hold.listDocuments.resolve();
      api.hold = {};
      api.fail = {};

      api.documents = [];
      await page.reload();
      await expect(page.getByText("No docs yet")).toBeVisible();
      await expectCleanLayout(page, "list empty", bothEnds);
    });

    test("list: filters, select mode and bulk lifecycle", async ({ page }) => {
      const api = await installDocsApi(page);
      await page.goto(docsUrl());
      await expect(page.getByText("Billing runbook")).toBeVisible();

      await page.getByTestId("docs-filters-toggle").click();
      await expect(page.getByTestId("docs-filter-panel")).toBeVisible();
      await expectCleanLayout(page, "filters open", bothEnds);

      await page.getByRole("checkbox", { name: "Archived" }).check();
      await page.getByRole("checkbox", { name: "Trashed" }).check();
      await page.getByRole("button", { name: "Apply" }).click();
      await expect(
        page.getByRole("button", { name: "Filtered" }),
      ).toBeVisible();
      await expectCleanLayout(page, "filters applied", bothEnds);

      await page.getByRole("button", { name: "Select", exact: true }).click();
      await expectCleanLayout(page, "select mode empty", bothEnds);

      await page.getByRole("button", { name: "Select all" }).click();
      await expect(
        page.getByRole("toolbar", { name: "Bulk actions" }),
      ).toBeVisible();
      await expectCleanLayout(page, "all selected", bothEnds);

      await page.getByRole("button", { name: "Archive", exact: true }).click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await expectCleanLayout(page, "bulk archive confirm", bothEnds);

      api.hold.archiveDocument = deferred();
      await page
        .getByRole("button", { name: "Archive", exact: true })
        .last()
        .click();
      await expectCleanLayout(page, "bulk archive running", bothEnds);
      api.hold.archiveDocument.resolve();
      api.hold = {};

      await page.getByRole("button", { name: "Select all" }).click();
      api.fail.trashDocument = {
        message: `trash refused: ${LONG_TOKEN} is referenced elsewhere `.repeat(
          3,
        ),
      };
      await page.getByRole("button", { name: "Move to trash" }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Trash" })
        .click();
      await expect(page.getByText(/trash refused/)).toBeVisible();
      await expectCleanLayout(page, "bulk trash failed", bothEnds);
    });

    test("list: create form, validation and failure", async ({ page }) => {
      const api = await installDocsApi(page);
      await page.goto(docsUrl());
      await expect(page.getByText("Billing runbook")).toBeVisible();

      await page.getByRole("button", { name: "New doc" }).click();
      await expect(
        page.getByRole("textbox", { name: "Head content (Markdown) *" }),
      ).toBeVisible();
      await expectCleanLayout(page, "create form open", bothEnds);

      await page.getByRole("button", { name: "Create doc" }).click();
      await expect(page.getByText("Title is required.")).toBeVisible();
      await expectCleanLayout(page, "create validation error", bothEnds);

      await page
        .getByPlaceholder("Document title", { exact: true })
        .fill(LONG_TITLE);
      await page
        .getByRole("textbox", { name: "Head content (Markdown) *" })
        .fill(DOC_BODY);
      await expectCleanLayout(page, "create form filled", bothEnds);

      api.fail.createDocument = {
        message:
          `document quota exceeded for this workspace (${LONG_TOKEN}) `.repeat(
            3,
          ),
      };
      await page.getByRole("button", { name: "Create doc" }).click();
      await expect(page.getByText(/document quota exceeded/)).toBeVisible();
      await expectCleanLayout(page, "create failed", bothEnds);

      api.fail = {};
      api.hold.createDocument = deferred();
      await page
        .getByRole("button", { name: "Creating…" })
        .or(page.getByRole("button", { name: "Create doc" }))
        .click();
      await expect(
        page.getByRole("button", { name: "Creating…" }),
      ).toBeVisible();
      await expectCleanLayout(page, "creating");
      api.hold.createDocument.resolve();
      api.hold = {};
      await expect(page).toHaveURL(/\/docs\/doc-created$/);
    });

    test("list: search and thread scope", async ({ page }) => {
      const api = await installDocsApi(page, { searchNextCursor: "cursor-2" });
      await page.goto(docsUrl());
      await expect(page.getByText("Billing runbook")).toBeVisible();

      await page
        .getByRole("searchbox", { name: "Search documents" })
        .fill(LONG_TOKEN);
      await page.getByRole("button", { name: "Search", exact: true }).click();
      await expect(page.getByText(/Showing the first/)).toBeVisible();
      await expectCleanLayout(page, "search truncated", bothEnds);

      api.searchResults = [];
      await page
        .getByRole("searchbox", { name: "Search documents" })
        .fill(`${LONG_TOKEN} ${LONG_HASH}`);
      await page.getByRole("button", { name: "Search", exact: true }).click();
      await expect(page.getByText("No matching docs")).toBeVisible();
      await expectCleanLayout(page, "search empty", bothEnds);

      await page.goto(docsUrl(`?thread_id=thread-${LONG_HASH}`));
      await expect(
        page.getByText(
          "Showing only documents on this backing thread timeline.",
        ),
      ).toBeVisible();
      await expectCleanLayout(page, "thread scoped", bothEnds);
    });

    test("detail: plain doc, revision history and old revision", async ({
      page,
    }) => {
      const api = await installDocsApi(page);
      api.hold.getDocument = deferred();
      await page.goto(`${DOCS_PATH}/doc-plain-notes`);
      await expect(page.getByText("Loading...")).toBeVisible();
      await expectCleanLayout(page, "detail loading");
      api.hold.getDocument.resolve();
      api.hold = {};

      await expect(
        page.getByRole("heading", { name: "Meeting notes" }).first(),
      ).toBeVisible();
      await expectCleanLayout(page, "detail plain", bothEnds);

      await page.getByRole("button", { name: "More actions" }).click();
      await expect(page.getByRole("menu")).toBeVisible();
      await expectCleanLayout(page, "more actions menu");

      api.hold.getDocumentHistory = deferred();
      await page.getByRole("menuitem", { name: "Revision history" }).click();
      await expect(page.getByText("Loading revision history...")).toBeVisible();
      await expectCleanLayout(page, "revision history loading", bothEnds);
      api.hold.getDocumentHistory.resolve();
      api.hold = {};

      await expect(page.getByText("Current version")).toBeVisible();
      await expectCleanLayout(page, "revision history open", bothEnds);

      await page.getByRole("button", { name: /Version 1/ }).click();
      await expect(page.getByText(/Viewing revision 1/)).toBeVisible();
      await expectCleanLayout(page, "old revision", bothEnds);

      await page.getByRole("button", { name: "Return to current" }).click();
      await expect(page.getByText(/Viewing revision 1/)).toHaveCount(0);
      await page.getByRole("button", { name: "Close history" }).click();
      await expectCleanLayout(page, "history closed", bothEnds);
    });

    test("detail: editor modes, dirty state and save failure", async ({
      page,
    }) => {
      const api = await installDocsApi(page);
      await page.goto(`${DOCS_PATH}/doc-plain-notes`);
      await expect(
        page.getByRole("heading", { name: "Meeting notes" }).first(),
      ).toBeVisible();

      await page.getByRole("button", { name: "Edit", exact: true }).click();
      await expect(
        page.getByRole("textbox", { name: "Content (Markdown)" }),
      ).toBeVisible();
      await expectCleanLayout(page, "editor split", bothEnds);

      await page.getByRole("tab", { name: "Write" }).click();
      await expectCleanLayout(page, "editor write", bothEnds);
      await page.getByRole("tab", { name: "Preview" }).click();
      await expectCleanLayout(page, "editor preview", bothEnds);
      await page.getByRole("tab", { name: "Split" }).click();

      await page
        .getByRole("textbox", { name: "Content (Markdown)" })
        .fill(DOC_BODY);
      await expect(page.getByText("Unsaved changes")).toBeVisible();
      await expectCleanLayout(page, "editor dirty", bothEnds);

      api.fail.updateDocument = {
        message: `revision conflict: base ${LONG_HASH} is stale `.repeat(3),
      };
      await page.getByRole("button", { name: "Save revision" }).click();
      await expect(page.getByText(/revision conflict/)).toBeVisible();
      await expectCleanLayout(page, "editor save failed", bothEnds);

      api.fail = {};
      api.hold.updateDocument = deferred();
      await page.getByRole("button", { name: "Save revision" }).click();
      await expect(page.getByRole("button", { name: "Saving…" })).toBeVisible();
      await expectCleanLayout(page, "editor saving", bothEnds);
      api.hold.updateDocument.resolve();
      api.hold = {};
      await expect(
        page.getByRole("textbox", { name: "Content (Markdown)" }),
      ).toHaveCount(0);
      await expectCleanLayout(page, "editor saved", bothEnds);
    });

    test("detail: archived, trashed and lifecycle confirms", async ({
      page,
    }) => {
      const api = await installDocsApi(page);
      await page.goto(`${DOCS_PATH}/doc-billing-runbook`);
      await expect(page.getByText(/This document was archived/)).toBeVisible();
      // No "on" before the timestamp: formatTimestamp is relative under 7 days
      // ("3h ago") and absolute beyond, so "archived on <stamp>" read wrong for
      // recent archives. See archived-copy-states.spec.js.
      await expect(page.getByText(/was archived on /)).toHaveCount(0);
      await expectCleanLayout(page, "archived banner", bothEnds);

      await page.getByRole("button", { name: "Unarchive" }).click();
      await expect(page.getByText(/This document was archived/)).toHaveCount(0);
      await expectCleanLayout(page, "unarchived", bothEnds);

      await page.getByRole("button", { name: "More actions" }).click();
      await page.getByRole("menuitem", { name: "Move to trash" }).click();
      await expect(
        page.getByRole("dialog", { name: "Move to trash" }),
      ).toBeVisible();
      await expectCleanLayout(page, "trash confirm", bothEnds);
      await page.getByRole("button", { name: "Cancel" }).click();

      await page.goto(`${DOCS_PATH}/doc-trashed-notes`);
      await expect(page.getByText("This document is in trash")).toBeVisible();
      await expectCleanLayout(page, "trashed banner", bothEnds);

      api.hold.restoreDocument = deferred();
      await page.getByRole("button", { name: "Restore" }).click();
      await expectCleanLayout(page, "restoring");
      api.hold.restoreDocument.resolve();
      api.hold = {};

      api.history = [];
      await page.goto(`${DOCS_PATH}/doc-structured`);
      await expect(page.getByText(/edit via CLI/)).toBeVisible();
      await expectCleanLayout(page, "structured doc", bothEnds);

      await page.getByRole("button", { name: "More actions" }).click();
      await page.getByRole("menuitem", { name: "Revision history" }).click();
      await expect(page.getByText("No earlier revisions found.")).toBeVisible();
      await expectCleanLayout(page, "empty revision history", bothEnds);
    });

    test("detail: load error and missing document", async ({ page }) => {
      const api = await installDocsApi(page);
      api.fail.getDocument = {
        message: `document store unavailable (${LONG_HASH}) `.repeat(4),
      };
      await page.goto(`${DOCS_PATH}/doc-plain-notes`);
      await expect(page.getByText(/document store unavailable/)).toBeVisible();
      await expectCleanLayout(page, "detail load error", bothEnds);

      api.fail = {};
      await page.goto(`${DOCS_PATH}/${LONG_HASH}`);
      await expect(page.getByText("Document not found.")).toBeVisible();
      await expectCleanLayout(page, "detail not found", bothEnds);
    });

    test("detail: threaded doc with discussion rail", async ({ page }) => {
      await installDocsApi(page);
      await page.addInitScript(() => {
        localStorage.setItem(
          "discussion-drawer:doc-discussion:doc-launch-checklist",
          "1",
        );
      });
      await page.goto(`${DOCS_PATH}/doc-launch-checklist`);
      await expect(
        page.getByRole("heading", { name: LONG_TITLE }).first(),
      ).toBeVisible();
      await expect(
        page.getByText("Keep support handoff visible in the intro."),
      ).toBeVisible();
      await expectCleanLayout(page, "rail open", bothEnds);

      await page.getByRole("tab", { name: "Revisions" }).click();
      await expect(page.getByText("Current version")).toBeVisible();
      await expectCleanLayout(page, "rail revisions tab", bothEnds);

      await page.getByRole("tab", { name: /^Discussion/ }).click();
      await expectCleanLayout(page, "rail discussion tab", bothEnds);

      // Collapsed rail (desktop) / collapsed dock (compact).
      await page
        .getByRole("button", { name: /Hide discussion|Tap to collapse/ })
        .click();
      await expectCleanLayout(page, "rail collapsed", bothEnds);
      await page
        .getByRole("button", { name: /Show discussion|Tap to expand/ })
        .first()
        .click();
      await expectCleanLayout(page, "rail reopened", bothEnds);

      // Share menu + kebab open over the rail.
      await page.getByRole("button", { name: "Copy document ref" }).click();
      await expectCleanLayout(page, "share menu open");
      await page.keyboard.press("Escape");
    });

    test("detail: rename and selection comment pill", async ({ page }) => {
      const api = await installDocsApi(page);
      await page.goto(`${DOCS_PATH}/doc-launch-checklist`);
      await expect(
        page.getByRole("heading", { name: LONG_TITLE }).first(),
      ).toBeVisible();

      await page.getByRole("button", { name: "Rename document" }).click();
      const titleInput = page.getByRole("textbox", { name: "Document title" });
      await expect(titleInput).toBeVisible();
      await titleInput.fill(`${LONG_TITLE} ${LONG_TOKEN}`);
      await expectCleanLayout(page, "title editing", bothEnds);

      api.fail.updateDocument = {
        message:
          `rename rejected: ${LONG_HASH} is not the head revision `.repeat(2),
      };
      await titleInput.press("Enter");
      await expect(page.getByText(/rename rejected/)).toBeVisible();
      await expectCleanLayout(page, "rename failed", bothEnds);
      api.fail = {};

      // Select a phrase in the rendered body: the floating Comment pill follows.
      await page.evaluate(() => {
        const root = document.querySelector(".js-doc-markdown-body");
        const target = Array.from(root.querySelectorAll("p, li, td")).find(
          (el) => el.textContent.includes("Check the OAuth callback copy"),
        );
        target.scrollIntoView({ block: "center", behavior: "instant" });
        const range = document.createRange();
        range.selectNodeContents(target);
        const selection = window.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        root.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
      });
      await expect(page.getByRole("button", { name: "Comment" })).toBeVisible();
      await expectCleanLayout(page, "selection pill");

      await page.getByRole("button", { name: "Comment" }).click();
      await expectCleanLayout(page, "pending doc comment", bothEnds);
    });

    test("settings page: load, save, conflict and trashed", async ({
      page,
    }) => {
      const api = await installDocsApi(page);
      api.hold.getDocument = deferred();
      await page.goto(`${DOCS_PATH}/doc-launch-checklist/edit`);
      await expect(page.getByText("Loading…")).toBeVisible();
      await expectCleanLayout(page, "settings loading");
      api.hold.getDocument.resolve();
      api.hold = {};

      const summary = page.getByRole("textbox", { name: "Short description" });
      await expect(summary).toBeVisible();
      await expectCleanLayout(page, "settings loaded", bothEnds);

      await summary.fill(`${LONG_SUMMARY} ${LONG_TOKEN}`);
      api.fail.patchDocument = {
        status: 409,
        message: "conflict",
      };
      await page.getByRole("button", { name: "Save" }).click();
      await expect(page.getByText(/updated elsewhere/)).toBeVisible();
      await expectCleanLayout(page, "settings conflict", bothEnds);

      api.fail.patchDocument = {
        message: `patch rejected for ${LONG_TOKEN} `.repeat(4),
      };
      await page.getByRole("button", { name: "Save" }).click();
      await expect(page.getByText(/patch rejected/)).toBeVisible();
      await expectCleanLayout(page, "settings save failed", bothEnds);
      api.fail = {};

      await page.goto(`${DOCS_PATH}/doc-trashed-notes/edit`);
      await expect(page.getByText(/This document is in trash/)).toBeVisible();
      await expectCleanLayout(page, "settings trashed", bothEnds);
    });

    test("revision resolver: loading and not found", async ({ page }) => {
      const api = await installDocsApi(page);
      api.hold.listDocuments = deferred();
      await page.goto(`${DOCS_PATH}/revisions/rev-${LONG_HASH}`);
      await expect(
        page.getByText("Resolving document revision…"),
      ).toBeVisible();
      await expectCleanLayout(page, "resolver loading");
      api.hold.listDocuments.resolve();
      api.hold = {};

      await expect(page.getByText(/was not found/)).toBeVisible();
      await expectCleanLayout(page, "resolver not found", bothEnds);
    });
  });
}
