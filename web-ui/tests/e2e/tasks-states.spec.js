import { expect, test } from "@playwright/test";

import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION } from "../../src/lib/config.js";
import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

/**
 * Walks the Tasks area (list table, board, filters, move notice, evidence
 * form, shortcut overlay, task detail, new task) through every UI state it can
 * reach and runs the geometry audit after each transition, at every audit
 * viewport.
 *
 * The fixture data is deliberately ugly: unbroken 80+ char identifiers, long
 * owner strings, multi-sentence source errors and a source phase Nexus has no
 * name for.
 */

const ROOT = "/o/local/w/local";
const TASKS = `${ROOT}/tasks`;

const LONG_ID =
  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
const LONG_TITLE = `Verify the rollout of anx-core-release-candidate-${LONG_ID} end to end`;
const LONG_OWNER = `agent_ext_${LONG_ID}`;
const LONG_ACTION =
  "Re-run the acceptance suite against the staging workspace and attach the evidence bundle before the release window closes";
const LONG_NATIVE_STATUS =
  "Waiting on downstream vendor acknowledgement (escalated, no ETA)";
const LONG_ERROR =
  "Read permission expired for installation 4820113 while listing issues; the stored credential no longer grants repo:read and every subsequent poll failed with the same response.";

const stamp = (hours = 0) =>
  new Date(
    Date.parse("2026-05-01T12:00:00.000Z") - hours * 3600000,
  ).toISOString();

function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function freshness(status, extra = {}) {
  if (status === "unknown") return { status: "unknown" };
  return {
    status,
    last_observed_at: stamp(status === "fresh" ? 0.1 : 30),
    stale_after_seconds: 3600,
    ...extra,
  };
}

function buildWork() {
  return [
    {
      ref: "card:release",
      handle: "release",
      title: LONG_TITLE,
      summary:
        "The release candidate has to be proven against staging before it is promoted. " +
        `Tracking identifier ${LONG_ID}.`,
      board_ref: "board:sample",
      project_ref: `topic:platform-release-readiness-${LONG_ID}`,
      owner: LONG_OWNER,
      priority: "P1",
      phase: "review",
      next_actor: LONG_OWNER,
      next_action: LONG_ACTION,
      definition_of_done: [
        "A sample operator can open the workspace and see the release note",
        `The evidence bundle artifact:${LONG_ID} is attached to this task`,
      ],
      source: {
        authority: "github",
        connection_id: "sample-github",
        native_id: "12",
        native_status: "Awaiting reviewer",
        url: "https://example.test/issues/12",
      },
      freshness: freshness("stale"),
      refresh: {
        state: "failed",
        last_error: LONG_ERROR,
        last_attempt_at: stamp(1),
        last_success_at: stamp(30),
        failures: 31,
      },
      blockers: [
        `The staging workspace is pinned to agent-nexus-${LONG_ID} which no longer builds`,
      ],
      wake_condition: "The staging workspace reports a green acceptance run",
      start_at: stamp(72),
      due_at: stamp(-48),
      relations: [
        { kind: "blocks", ref: "card:docs" },
        { kind: "evidence", ref: `artifact:${LONG_ID}` },
      ],
      executions: [
        {
          authority: "github",
          run_id: `run_${LONG_ID}`,
          url: "https://example.test/runs/1",
          host: "m4-hermes",
          harness: "claude-code",
          agent: "release-verifier",
          model: "claude-opus-5",
          result_ref: `artifact:${LONG_ID}`,
        },
      ],
    },
    {
      ref: "card:docs",
      handle: "docs",
      title: "Document the sample outcome",
      board_ref: "board:sample",
      source: { authority: "nexus", native_status: "Ready" },
      phase: "in_progress",
      owner: "Sample Owner",
      next_actor: "Writer",
      next_action: "Draft acceptance examples",
      freshness: freshness("unknown"),
      definition_of_done: ["Examples reviewed"],
    },
    {
      ref: "card:vendor",
      handle: "vendor",
      title: "Vendor sample delivery",
      board_ref: "board:sample",
      source: {
        authority: "multica",
        connection_id: "sample-multica",
        native_status: LONG_NATIVE_STATUS,
      },
      // A phase Nexus has no name for: the board grows an extra column.
      phase: "vendor_waiting",
      next_actor: "Vendor",
      freshness: freshness("fresh"),
    },
    {
      ref: "card:blocked",
      handle: "blocked",
      title: "Unblock the packed-host updater",
      board_ref: "board:sample",
      source: { authority: "nexus" },
      phase: "blocked",
      priority: "critical",
      owner: "riley@example.com",
      freshness: freshness("unknown"),
    },
    {
      ref: "card:unreachable",
      handle: "unreachable",
      title: `Reconcile ${LONG_ID}`,
      board_ref: "board:archive",
      source: {
        authority: "github",
        connection_id: "sample-github",
        native_id: "998",
      },
      phase: "ready",
      owner: LONG_OWNER,
      freshness: {
        status: "error",
        last_observed_at: stamp(400),
        stale_after_seconds: 3600,
        error: LONG_ERROR,
      },
      refresh: { state: "failed", last_error: LONG_ERROR },
    },
    {
      ref: "card:never",
      handle: "never",
      title: "Never checked source item",
      board_ref: "board:archive",
      source: { authority: "git", native_id: LONG_ID },
      phase: "backlog",
      freshness: freshness("unknown"),
    },
    {
      ref: "card:done",
      handle: "done",
      title: "Ship the workspace tour",
      board_ref: "board:sample",
      source: { authority: "nexus" },
      phase: "done",
      owner: "Sample Owner",
      freshness: freshness("fresh"),
    },
    {
      ref: "card:cancelled",
      handle: "cancelled",
      title: "Retire the legacy decisions route",
      board_ref: "board:sample",
      source: { authority: "nexus" },
      phase: "cancelled",
      freshness: freshness("unknown"),
    },
    ...Array.from({ length: 6 }, (_, i) => ({
      ref: `card:backlog-${i}`,
      handle: `backlog-${i}`,
      title: `Backlog item ${i} — ${"refine the acceptance criteria ".repeat(2)}`,
      board_ref: "board:sample",
      source: { authority: "nexus" },
      phase: "backlog",
      owner: i % 2 ? LONG_OWNER : "Sample Owner",
      next_actor: "Sample Owner",
      next_action: LONG_ACTION,
      freshness: freshness("unknown"),
    })),
  ];
}

const OBSERVATIONS = [
  {
    id: "observation-verified",
    reader_id: `reader_${LONG_ID}`,
    reader_revision: "v1.12.3",
    source_revision: LONG_ID,
    observed_at: stamp(30),
    received_at: stamp(30),
    status: "verified",
    verification: "verified",
    facts: { native_status: "Awaiting reviewer" },
    evidence: [
      {
        url: "https://example.test/evidence/the-acceptance-run-that-proves-it",
        summary: `Acceptance run ${LONG_ID}`,
      },
      { ref: `artifact:${LONG_ID}` },
    ],
    coverage: { complete: true },
  },
  {
    id: "observation-uncertain",
    reader_id: "sample-reader",
    observed_at: stamp(31),
    status: "uncertain",
    verification: "reported",
    facts: {},
    uncertainty: [
      `Serving revision not observed; the reader only saw ${LONG_ID}`,
    ],
    coverage: { complete: false },
  },
  ...Array.from({ length: 4 }, (_, i) => ({
    id: `observation-error-${i}`,
    reader_id: "sample-reader",
    observed_at: stamp(32 + i),
    status: "error",
    error: { message: LONG_ERROR },
    facts: {},
  })),
];

function buildDecisions() {
  return [
    {
      id: `decision-${LONG_ID}`,
      work_ref: "card:release",
      instruction: `request status change at GitHub to In review`,
      scope: "work.phase",
      target_revision: "7",
      status: "awaiting_answer",
      revision: 2,
      created_at: stamp(2),
      payload: { phase: "review" },
    },
  ];
}

/**
 * Mutable mock of every endpoint the Tasks area touches. Endpoints read their
 * behavior from `api` at request time, so a test flips a field and drives the
 * UI into the state it wants:
 *   - `hold.<name>`: a deferred the response waits on (in-flight states)
 *   - `fail.<name>`: respond with an error body
 */
async function installTasksApi(page, overrides = {}) {
  const digest = await getExpectedCommandRegistryDigest();
  const api = {
    work: buildWork(),
    workNextCursor: "",
    boards: [
      {
        ref: "board:sample",
        title: "Sample board",
        updated_at: stamp(1),
      },
      {
        ref: "board:archive",
        // A board title long enough to fight the Board column for room.
        title: `Archive of everything nobody owns anymore (${LONG_ID})`,
        updated_at: stamp(1),
      },
    ],
    decisions: buildDecisions(),
    actions: [],
    artifacts: Array.from({ length: 4 }, (_, i) => ({
      id: `${LONG_ID.slice(0, 24)}${i}`,
      ref: `artifact:${LONG_ID.slice(0, 24)}${i}`,
      title: "Vertical Slice Brief",
      created_at: stamp(i + 1),
    })),
    observations: OBSERVATIONS,
    observationsNextCursor: "",
    hold: {},
    fail: {},
    calls: [],
    ...overrides,
  };

  await page.addInitScript(() => {
    localStorage.setItem("workspaceTourSeen.local", "1");
    localStorage.setItem("workspaceTourSeen.local:local", "1");
  });

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = decodeURIComponent(url.pathname);
    const method = request.method();
    if (
      request.isNavigationRequest() ||
      path.startsWith("/o/") ||
      path.startsWith("/@") ||
      path.startsWith("/src/") ||
      path.startsWith("/node_modules/") ||
      path.startsWith("/.svelte-kit/") ||
      path.includes("__data.json")
    )
      return route.continue();

    const json = (status, body) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    const respond = async (name, okBody, okStatus = 200) => {
      api.calls.push({ name, path, method });
      if (api.hold[name]) await api.hold[name].promise;
      const failure = api.fail[name];
      if (failure)
        return json(
          failure.status ?? 500,
          failure.body ?? {
            error: {
              code: "test_failure",
              message: failure.message ?? String(failure),
              details: failure.message ?? String(failure),
            },
          },
        );
      return json(okStatus, typeof okBody === "function" ? okBody() : okBody);
    };

    if (path === "/meta/handshake" || path === "/version")
      return json(200, {
        schema_version: EXPECTED_SCHEMA_VERSION,
        command_registry_digest: digest,
        dev_actor_mode: false,
        human_auth_mode: "workspace_local",
      });
    if (path === "/auth/session")
      return json(200, {
        authenticated: true,
        agent: {
          agent_id: "human-sample",
          actor_id: "actor-sample",
          username: "Synthetic User",
          principal_kind: "human",
          auth_method: "passkey",
        },
      });
    if (path === "/actors")
      return json(200, {
        actors: [
          {
            id: "actor-sample",
            display_name: "Synthetic User",
            tags: ["human"],
          },
        ],
      });
    if (path === "/auth/principals")
      return json(200, { principals: [], next_cursor: "" });
    if (path === "/auth/bootstrap/status")
      return json(200, { bootstrap_required: false });
    if (path === "/home/unread")
      return json(200, {
        groups: [],
        unread_count: 0,
        group_count: 0,
        generated_at: stamp(),
      });
    if (path === "/inbox") return json(200, { items: [], total: 0 });
    if (path === "/docs" || path === "/docs/search")
      return json(200, { documents: [] });
    if (path === "/artifacts")
      return respond("artifacts", () => ({ artifacts: api.artifacts }));
    if (path === "/boards")
      return respond("boards", () => ({ boards: api.boards }));
    if (path.startsWith("/boards/"))
      return respond("board", () => ({
        board: api.boards.find((b) => b.ref === path.slice(8)) || api.boards[0],
      }));
    if (/^\/cards\/.+\/move$/.test(path))
      return respond("moveCard", () => ({ ok: true }));
    if (path === "/work/capabilities")
      return json(200, {
        capabilities: {
          refresh_executor_configured: false,
          observation_submission: true,
        },
      });
    if (path === "/work" && method === "GET")
      return respond("work", () => {
        const source = url.searchParams.get("source");
        const phase = url.searchParams.get("phase");
        const query = (url.searchParams.get("q") || "").toLowerCase();
        return {
          work: api.work.filter(
            (item) =>
              (!source || item.source?.authority === source) &&
              (!phase || (item.phase || "unknown") === phase) &&
              (!query || item.title.toLowerCase().includes(query)),
          ),
          next_cursor: api.workNextCursor,
        };
      });
    if (path === "/work" && method === "POST")
      return respond(
        "createWork",
        () => ({
          work: {
            ...request.postDataJSON(),
            ref: "card:created",
            handle: "created",
          },
        }),
        201,
      );
    if (path.endsWith("/observations"))
      return respond("observations", () => ({
        observations: api.observations,
        next_cursor: api.observationsNextCursor,
      }));
    if (path.endsWith("/refresh"))
      return respond("refresh", () => ({ refresh: { state: "queued" } }), 202);
    if (path.startsWith("/work/"))
      return respond("getWork", () => ({
        work: api.work.find((item) => item.ref === path.slice(6)) || null,
      }));
    if (path === "/pm/decisions" && method === "POST")
      return respond(
        "createDecision",
        () => {
          const body = request.postDataJSON();
          const decision = {
            id: `decision-created-${api.decisions.length}`,
            ...body,
            status: "awaiting_answer",
            created_at: stamp(),
          };
          api.decisions = [...api.decisions, decision];
          return decision;
        },
        201,
      );
    if (path === "/pm/decisions")
      return respond("decisions", () => ({
        items: api.decisions,
        has_more: false,
      }));
    if (path === "/pm/actions")
      return respond("actions", () => ({
        items: api.actions,
        has_more: false,
      }));
    if (path.startsWith("/pm/") || path.startsWith("/work"))
      return json(404, { error: { message: "Unconfigured synthetic route" } });
    return route.continue();
  });

  return api;
}

const bothEnds = { scrollPositions: ["top", "bottom"] };
// The dev server is shared with other suites; a cold first paint can take a
// while, so the first assertion after a navigation waits longer.
const firstPaint = { timeout: 20000 };

/** Focus a board card by ref so the arrow-key move path can be driven. */
async function focusCard(page, ref) {
  await page
    .locator(`[data-work-ref="${ref}"][tabindex="0"]`)
    .evaluate((el) => el.focus());
}

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`tasks states @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
    });

    test("table: loading, populated, reload failure and empty", async ({
      page,
    }) => {
      const api = await installTasksApi(page);
      api.hold.work = deferred();
      await page.goto(TASKS);
      await expect(page.getByText("Loading tasks…")).toBeVisible(firstPaint);
      await expectCleanLayout(page, "initial loading");

      api.hold.work.resolve();
      api.hold = {};
      await expect(page.getByRole("link", { name: LONG_TITLE })).toBeVisible();
      await expectCleanLayout(page, "populated table", bothEnds);

      // Refresh over populated data: the rows must stay put.
      api.hold.work = deferred();
      await page.getByRole("button", { name: "Reload" }).click();
      await expectCleanLayout(page, "reloading over populated table");
      api.hold.work.resolve();
      api.hold = {};
      await expect(page.getByRole("button", { name: "Reload" })).toBeEnabled();

      // A failed reload keeps the last good rows plus a warning.
      api.fail.work = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Reload" }).click();
      await expect(
        page.getByText("Showing the previously loaded records.", {
          exact: false,
        }),
      ).toBeVisible();
      await expectCleanLayout(page, "reload failed over stale rows", bothEnds);

      // And with nothing loaded at all.
      await page.reload();
      await expect(page.getByText("Tasks could not be refreshed")).toBeVisible(
        firstPaint,
      );
      await expectCleanLayout(page, "load failed with no rows", bothEnds);

      api.fail = {};
      api.work = [];
      await page.getByRole("button", { name: "Retry" }).click();
      await expect(page.getByText("Nothing tracked yet")).toBeVisible();
      await expectCleanLayout(page, "empty workspace", bothEnds);
    });

    test("filters, search and pagination", async ({ page }) => {
      const api = await installTasksApi(page, { workNextCursor: "cursor-2" });
      await page.goto(TASKS);
      await expect(page.getByRole("link", { name: LONG_TITLE })).toBeVisible(
        firstPaint,
      );
      await expectCleanLayout(page, "load more available", bothEnds);

      api.hold.work = deferred();
      await page.getByRole("button", { name: "Load more" }).click();
      await expectCleanLayout(page, "loading more");
      api.hold.work.resolve();
      api.hold = {};

      const filters = page
        .getByRole("group")
        .filter({ hasText: "Filters" })
        .locator("summary");
      await filters.click();
      await expectCleanLayout(page, "filters open", bothEnds);

      await page.getByLabel("Source", { exact: true }).selectOption("github");
      await expect(page).toHaveURL(/source=github/);
      await expect(page.locator("[data-work-ref]")).toHaveCount(2);
      await expectCleanLayout(page, "source filtered");

      await page.getByLabel("Status", { exact: true }).selectOption("blocked");
      await page.getByLabel("Freshness", { exact: true }).selectOption("error");
      await page
        .getByLabel("Project reference")
        .fill(`topic:platform-release-readiness-${LONG_ID}`);
      await page.getByLabel("Owner", { exact: true }).fill(LONG_OWNER);
      await page.getByRole("button", { name: "Apply" }).click();
      await expect(page.getByText("No matching tasks")).toBeVisible();
      await expectCleanLayout(
        page,
        "every filter active, no matches",
        bothEnds,
      );

      await page.getByRole("link", { name: "Clear filters" }).first().click();
      await expect(page.getByRole("link", { name: LONG_TITLE })).toBeVisible();

      await page
        .getByRole("searchbox", { name: "Search tasks" })
        .fill(`no-such-task-${LONG_ID}`);
      await page.keyboard.press("Enter");
      await expect(page.getByText("No matching tasks")).toBeVisible();
      await expectCleanLayout(page, "search with no matches");
    });

    test("board: populated, requested badge and drag overlay", async ({
      page,
    }) => {
      await installTasksApi(page);
      await page.goto(`${TASKS}?view=board`);
      await expect(
        page.getByRole("region", { name: "Task board grouped by phase" }),
      ).toBeVisible(firstPaint);
      await expect(
        page.locator('[data-work-ref="card:release"]'),
      ).toBeVisible();
      await expectCleanLayout(page, "populated board", bothEnds);

      // A card lifted under the pointer paints a fixed overlay over the board.
      // The first column is the only one on screen at phone width.
      const card = page.locator("[data-work-slot][data-work-ref]").first();
      await card.scrollIntoViewIfNeeded();
      const from = await card.boundingBox();
      await page.mouse.move(from.x + from.width / 2, from.y + 12);
      await page.mouse.down();
      await page.mouse.move(from.x + from.width / 2 + 60, from.y + 90, {
        steps: 8,
      });
      await expect(page.locator("[data-work-drag-overlay]")).toBeVisible();
      await expectCleanLayout(page, "card lifted under the pointer");
      await page.keyboard.press("Escape");
      await page.mouse.up();
    });

    test("board: move notice, undo, evidence form and move errors", async ({
      page,
    }) => {
      const api = await installTasksApi(page);
      page.on("dialog", (dialog) => dialog.accept());
      await page.goto(`${TASKS}?view=board`);
      await expect(page.locator('[data-work-ref="card:docs"]')).toBeVisible(
        firstPaint,
      );

      // Nexus-owned move: in_progress -> review, with an Undo affordance.
      await focusCard(page, "card:docs");
      await page.keyboard.press("ArrowRight");
      const notice = page.locator("[data-work-move-notice]");
      await expect(notice).toBeVisible();
      await expect(notice.getByRole("button", { name: "Undo" })).toBeVisible();
      await expectCleanLayout(page, "move notice with undo", bothEnds);

      // Source-owned move files a request: the longest notice the page has.
      await focusCard(page, "card:unreachable");
      await page.keyboard.press("ArrowRight");
      await expect(
        notice.getByRole("link", { name: "Open in Inbox" }),
      ).toBeVisible();
      await expectCleanLayout(page, "requested move notice", bothEnds);

      // The shortcut overlay can be opened while the notice is still up.
      await page.keyboard.press("?");
      await expect(
        page.getByRole("dialog", { name: "Keyboard shortcuts" }),
      ).toBeVisible();
      await expectCleanLayout(page, "shortcut overlay over move notice");
      await page.keyboard.press("Escape");
      await expect(
        page.getByRole("dialog", { name: "Keyboard shortcuts" }),
      ).toHaveCount(0);
      await notice.getByRole("button", { name: "Dismiss" }).click();
      await expect(notice).toHaveCount(0);

      // Done needs evidence: an inline form appears above the board.
      await focusCard(page, "card:docs");
      await page.keyboard.press("ArrowRight");
      const evidence = page
        .getByRole("status")
        .filter({ hasText: "needs evidence" });
      await expect(evidence).toBeVisible();
      await expectCleanLayout(page, "evidence form open", bothEnds);

      // Core rejects a ref that does not exist; the typed value stays put.
      api.fail.moveCard = {
        status: 422,
        body: {
          error: {
            code: "invalid_argument",
            message: `missing or trashed resolution ref "artifact:${LONG_ID}"`,
            details: `missing or trashed resolution ref "artifact:${LONG_ID}"`,
          },
        },
      };
      await page.getByLabel(/needs evidence/).fill(`artifact:${LONG_ID}`);
      await page.getByRole("button", { name: "Mark done" }).click();
      await expect(page.getByText(/Nothing exists at artifact:/)).toBeVisible();
      await expectCleanLayout(page, "evidence rejected", bothEnds);

      api.fail = {};
      await page.getByRole("button", { name: "Mark done" }).click();
      await expect(evidence).toHaveCount(0);
      await expect(notice).toBeVisible();
      await expectCleanLayout(page, "done applied", bothEnds);

      // The same form for a source task whose title is one long token.
      await notice.getByRole("button", { name: "Dismiss" }).click();
      await focusCard(page, "card:release");
      await page.keyboard.press("ArrowRight");
      await expect(evidence).toBeVisible();
      await expect(evidence).toContainText(LONG_TITLE);
      await expectCleanLayout(
        page,
        "evidence form with a long title",
        bothEnds,
      );
    });

    test("new task: loading, no boards, filled form and save failure", async ({
      page,
    }) => {
      const api = await installTasksApi(page);
      page.on("dialog", (dialog) => dialog.accept());
      api.hold.boards = deferred();
      await page.goto(`${TASKS}/new`);
      await expect(page.getByText("Loading…")).toBeVisible(firstPaint);
      await expectCleanLayout(page, "new task loading");

      api.hold.boards.resolve();
      api.hold = {};
      await expect(page.getByLabel("Outcome")).toBeVisible();
      await expect(page.getByLabel("Board")).toBeVisible();
      await expectCleanLayout(page, "new task empty form", bothEnds);

      await page.getByLabel("Outcome").fill(LONG_TITLE);
      await page.getByLabel("Board").selectOption("board:archive");
      await page.getByLabel("Context").fill(`${LONG_ERROR}\n${LONG_ID}`);
      await page.getByLabel("Acceptance criteria").fill(LONG_ACTION);
      await page.getByLabel("Accountable owner").fill(LONG_OWNER);
      await page.getByLabel("Next actor").fill(LONG_OWNER);
      await page.getByLabel("Next action").fill(LONG_ACTION);
      await expectCleanLayout(page, "new task filled form", bothEnds);

      api.hold.createWork = deferred();
      await page.getByRole("button", { name: "Create task" }).click();
      await expect(
        page.getByRole("button", { name: "Creating…" }),
      ).toBeVisible();
      await expectCleanLayout(page, "new task saving");

      api.fail.createWork = { message: LONG_ERROR };
      api.hold.createWork.resolve();
      api.hold = {};
      await expect(page.getByRole("alert")).toContainText(
        "Read permission expired",
      );
      await expectCleanLayout(page, "new task save failed", bothEnds);

      // Zero boards is not a dead end: the form stays, and Board is hidden
      // because the operator is not choosing among boards.
      api.fail = {};
      api.boards = [];
      for (const field of [
        "Outcome",
        "Context",
        "Acceptance criteria",
        "Accountable owner",
        "Next actor",
        "Next action",
      ])
        await page.getByLabel(field, { exact: true }).fill("");
      await page.reload();
      await expect(page.getByLabel("Outcome")).toBeVisible(firstPaint);
      await expect(page.getByLabel("Board")).toHaveCount(0);
      await expect(
        page.getByText("This workspace has no board yet"),
      ).toHaveCount(0);
      await expectCleanLayout(page, "new task with no boards", bothEnds);
    });

    test("task detail: loading, populated, disclosures and errors", async ({
      page,
    }) => {
      const api = await installTasksApi(page, {
        observationsNextCursor: "cursor-2",
      });
      api.hold.getWork = deferred();
      api.hold.observations = deferred();
      await page.goto(`${TASKS}/card%3Arelease`);
      await expect(page.getByText("Loading…")).toBeVisible(firstPaint);
      await expectCleanLayout(page, "detail loading");

      api.hold.getWork.resolve();
      api.hold.observations.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: LONG_TITLE }),
      ).toBeVisible();
      await expectCleanLayout(page, "detail populated", bothEnds);

      await page.getByRole("button", { name: "Check GitHub now" }).click();
      await expect(page.getByText(/Refresh queued/)).toBeVisible();
      await expectCleanLayout(page, "detail refresh queued", bothEnds);

      await page.getByText("Details", { exact: true }).click();
      await expect(page.getByRole("heading", { name: "Runs" })).toBeVisible();
      await expectCleanLayout(page, "detail details disclosure open", bothEnds);

      await page.getByRole("button", { name: "Older observations" }).click();
      await expectCleanLayout(page, "detail older observations", bothEnds);

      api.hold.getWork = deferred();
      await page.getByRole("button", { name: "Reload" }).click();
      await expect(page.getByText("Loading…")).toBeVisible();
      await expectCleanLayout(page, "detail reloading");
      api.hold.getWork.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: LONG_TITLE }),
      ).toBeVisible();

      // Evidence and decisions fail independently of the task itself.
      api.fail.observations = { message: LONG_ERROR };
      api.fail.decisions = { message: LONG_ERROR };
      await page.reload();
      await expect(page.getByText("Evidence history unavailable")).toBeVisible(
        firstPaint,
      );
      await expectCleanLayout(page, "detail section errors", bothEnds);

      api.fail = { getWork: { status: 404, message: LONG_ERROR } };
      await page.reload();
      await expect(page.getByText("Task unavailable")).toBeVisible(firstPaint);
      await expectCleanLayout(page, "detail unavailable", bothEnds);
    });

    test("task detail: nexus-owned minimal record", async ({ page }) => {
      await installTasksApi(page, { observations: [] });
      await page.goto(`${TASKS}/card%3Avendor`);
      await expect(
        page.getByRole("heading", { name: "Vendor sample delivery" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "detail sparse record", bothEnds);

      await page.goto(`${TASKS}/card%3Ablocked`);
      await expect(
        page.getByRole("heading", { name: "Unblock the packed-host updater" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "detail nexus owned", bothEnds);
    });
  });
}
