import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION } from "../../src/lib/config.js";

const root = "/o/local/w/local";
const stamp = (hours = 0) =>
  new Date(Date.now() - hours * 3600000).toISOString();
function records() {
  return [
    {
      ref: "card:release",
      handle: "release",
      title: "Release the sample workspace",
      summary: "Synthetic acceptance fixture",
      board_ref: "board:sample",
      source: {
        authority: "github",
        connection_id: "sample-github",
        native_id: "12",
        native_status: "Awaiting reviewer",
        url: "https://example.test/issues/12",
      },
      phase: "review",
      project_ref: "topic:sample",
      next_actor: "Reviewer",
      next_action: "Verify the release evidence",
      owner: "Sample Owner",
      priority: "P1",
      definition_of_done: ["A sample user can open the workspace"],
      freshness: {
        status: "stale",
        last_observed_at: stamp(3),
        source_activity_at: stamp(4),
        meaningful_progress_at: stamp(12),
        stale_after_seconds: 3600,
      },
      refresh: {
        state: "failed",
        last_error: "Read permission expired",
        last_attempt_at: stamp(1),
        last_success_at: stamp(3),
      },
    },
    {
      ref: "card:docs",
      handle: "docs",
      title: "Document the sample outcome",
      source: { authority: "nexus", native_status: "Ready" },
      phase: "in_progress",
      next_actor: "Writer",
      next_action: "Draft acceptance examples",
      freshness: { status: "unknown" },
      definition_of_done: ["Examples reviewed"],
    },
    {
      ref: "card:vendor",
      handle: "vendor",
      title: "Vendor sample delivery",
      source: {
        authority: "multica",
        connection_id: "sample-multica",
        native_status: "Custom waiting state",
      },
      phase: "vendor_waiting",
      next_actor: "Vendor",
      freshness: {
        status: "fresh",
        last_observed_at: stamp(),
        stale_after_seconds: 3600,
      },
    },
  ];
}
async function setup(page, overrides = {}) {
  const work = records();
  const calls = [];
  const digest = await getExpectedCommandRegistryDigest();
  const decisions = [
    {
      id: "decision-sample",
      work_ref: "card:release",
      instruction: "Update the sample handoff note",
      scope: "work.annotate",
      target_revision: "7",
      status: "awaiting_answer",
      revision: 2,
      created_at: stamp(2),
    },
  ];
  const documents = [
    {
      id: "document-runbook",
      handle: "release-runbook",
      ref: "document:release-runbook",
      title: "Release runbook",
      summary: "How to cut a workspace release safely.",
      source: "https://example.test/runbook.md",
      tags: ["knowledge", "ops"],
      state: "active",
      head_revision_number: 3,
      revision_count: 3,
      timeline_message_count: 2,
      updated_at: stamp(4),
      last_comment: {
        body: "Updated the rollback step after the last incident.",
        created_at: stamp(3),
        created_by: "actor-sample",
      },
    },
    {
      id: "document-notes",
      handle: "standup-notes",
      ref: "document:standup-notes",
      title: "Standup notes",
      summary: "Rolling operator notes.",
      tags: [],
      state: "active",
      head_revision_number: 1,
      updated_at: stamp(26),
    },
  ];
  const actions = [];
  const conversations = [];
  const turns = [];
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
    const reply = (body, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (path === "/meta/handshake" || path === "/version")
      return reply({
        schema_version: EXPECTED_SCHEMA_VERSION,
        command_registry_digest: digest,
        dev_actor_mode: false,
        human_auth_mode: "workspace_local",
      });
    if (path === "/auth/session")
      return reply({
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
      return reply({
        actors: [
          {
            id: "actor-sample",
            display_name: "Synthetic User",
            tags: ["human"],
          },
        ],
      });
    if (path === "/auth/principals")
      return reply({ principals: [], next_cursor: "" });
    if (path === "/auth/bootstrap/status")
      return reply({ bootstrap_required: false });
    if (path === "/home/unread")
      return reply({
        groups: [],
        unread_count: 0,
        group_count: 0,
        generated_at: stamp(),
      });
    if (path === "/inbox") return reply({ items: [], total: 0 });
    if (path === "/boards")
      return reply({
        boards: [{ ref: "board:sample", title: "Sample board" }],
      });
    if (path === "/docs" || path === "/docs/search")
      return reply({ documents });
    if (path.startsWith("/docs/") && path.endsWith("/revisions"))
      return reply({ revisions: [] });
    if (path.startsWith("/docs/"))
      return reply({
        document: documents[0],
        revision: {
          revision_id: "revision-runbook-3",
          revision_number: 3,
          content:
            "# Release runbook\n\n1. Freeze the board\n2. Run the checks\n3. Tag and announce",
          created_at: stamp(4),
        },
      });
    if (
      path === "/work" ||
      path.startsWith("/work/") ||
      path === "/pm" ||
      path.startsWith("/pm/")
    ) {
      const body = method === "GET" ? null : request.postDataJSON();
      calls.push({ path, method, body, query: url.searchParams });
      if (
        overrides.handle &&
        (await overrides.handle({
          path,
          method,
          body,
          reply,
          work,
          decisions,
          actions,
          conversations,
          turns,
          calls,
        }))
      )
        return;
      if (path === "/work/capabilities")
        return reply({
          capabilities: {
            refresh_executor_configured: false,
            observation_submission: true,
          },
        });
      if (path === "/work" && method === "GET")
        return reply({
          work: work.filter(
            (item) =>
              (!url.searchParams.get("source") ||
                item.source.authority === url.searchParams.get("source")) &&
              (!url.searchParams.get("q") ||
                item.title
                  .toLowerCase()
                  .includes(url.searchParams.get("q").toLowerCase())),
          ),
          next_cursor: "",
        });
      if (path === "/work" && method === "POST")
        return reply(
          { work: { ...body, ref: "card:created", handle: "created" } },
          201,
        );
      if (path.endsWith("/observations"))
        return reply({
          observations: [
            {
              id: "observation-sample",
              reader_id: "sample-reader",
              reader_revision: "v1",
              observed_at: stamp(3),
              status: "verified",
              verification: "reported",
              facts: { native_status: "Awaiting reviewer" },
              evidence: [
                {
                  url: "https://example.test/evidence",
                  summary: "Sample test result",
                },
              ],
              uncertainty: ["Serving revision not observed"],
              coverage: { complete: false },
            },
          ],
          next_cursor: "",
        });
      if (path.endsWith("/refresh"))
        return reply({ refresh: { state: "queued" } }, 202);
      if (path.startsWith("/work/"))
        return reply({
          work: work.find((item) => item.ref === path.slice(6)) || {
            ref: "card:created",
            title: "Created sample",
            source: { authority: "nexus" },
          },
        });
      if (path === "/pm/decisions" && method === "POST") {
        const item = {
          id: "decision-created",
          ...body,
          status: "awaiting_answer",
        };
        decisions.push(item);
        return reply(item, 201);
      }
      if (path === "/pm/decisions")
        return reply({ items: decisions, has_more: false });
      if (path.startsWith("/pm/decisions/") && method === "GET")
        return reply(
          decisions.find((item) => path.endsWith(item.id)) || decisions[0],
        );
      if (path === "/pm/actions")
        return reply({ items: actions, has_more: false });
      if (path.endsWith("/answer")) {
        decisions[0] = {
          ...decisions[0],
          status: "answered",
          answer: body.text,
          action_id: "action-sample",
          revision: 3,
        };
        actions.push({
          id: "action-sample",
          decision_id: decisions[0].id,
          status: "pending_delivery",
          receipt: {},
          attempts: [],
        });
        return reply(decisions[0]);
      }
      if (path.startsWith("/pm/actions/")) return reply(actions[0]);
      if (path === "/pm/conversations" && method === "GET")
        return reply({ items: conversations, has_more: false });
      if (path === "/pm/conversations" && method === "POST") {
        const item = {
          id: "conversation-sample",
          title: body.title,
          work_ref: body.work_ref,
          created_at: stamp(),
        };
        conversations.push(item);
        return reply(item, 201);
      }
      if (path.endsWith("/messages")) {
        const turn = {
          id: "turn-sample",
          text: body.text,
          status: "sending",
          created_at: stamp(),
        };
        turns.push(turn);
        return reply(turn, 202);
      }
      if (path.startsWith("/pm/conversations/"))
        return reply({ conversation: conversations[0], turns });
      return reply({ error: { message: "Unconfigured synthetic route" } }, 404);
    }
    return route.continue();
  });
  return { work, calls, decisions, actions, conversations, turns };
}

test("a reply that proposes decisions shows answerable rows linked to Inbox", async ({
  page,
}) => {
  const { conversations, turns } = await setup(page);
  conversations.push({
    id: "conversation-sample",
    title: "Weekly review",
    created_at: stamp(1),
  });
  turns.push({
    id: "turn-sample",
    text: "What needs my decision?",
    response:
      "One item needs you: the release note handoff. Proposed as decision:decision-sample.",
    status: "delivered",
    created_at: stamp(1),
    evidence_refs: ["card:release", "decision:decision-sample"],
  });
  await page.goto(`${root}/pm?conversation=conversation-sample`);
  const list = page.getByRole("list", {
    name: "Decisions proposed in this reply",
  });
  await expect(list).toBeVisible();
  const row = list.getByRole("listitem");
  await expect(row).toContainText("Update the sample handoff note");
  await expect(row.getByText("card:release")).toBeVisible();
  await expect(row.getByRole("link", { name: "Answer" })).toHaveAttribute(
    "href",
    "/o/local/w/local/inbox?item=decision:decision-sample",
  );
});

test("board and table preserve source states and show the same commitments", async ({
  page,
}) => {
  const { calls } = await setup(page);
  await page.goto(`${root}/tasks`);
  await expect(
    page.getByRole("link", {
      name: "Release the sample workspace",
      exact: true,
    }),
  ).toBeVisible();
  const tableRefs = await page
    .locator("[data-work-ref]")
    .evaluateAll((rows) => rows.map((row) => row.dataset.workRef).sort());
  await expect(
    page.getByText("Custom waiting state", { exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Board", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "Task board grouped by phase" }),
  ).toBeVisible();
  expect(
    await page
      .locator("[data-work-ref]")
      .evaluateAll((rows) => rows.map((row) => row.dataset.workRef).sort()),
  ).toEqual(tableRefs);
  // Source now lives inside the single Filters disclosure.
  await page
    .getByRole("group")
    .filter({ hasText: "Filters" })
    .locator("summary")
    .click();
  await page.getByLabel("Source", { exact: true }).selectOption("github");
  await expect(page.locator("[data-work-ref]")).toHaveCount(1);
  expect(calls.filter((call) => call.method !== "GET")).toHaveLength(0);
});

test("failed refresh retains last-good evidence and never promotes a claim to verified", async ({
  page,
}) => {
  const { calls } = await setup(page);
  await page.goto(`${root}/tasks/card%3Arelease`);
  await expect(
    page.getByRole("heading", { name: "Release the sample workspace" }),
  ).toBeVisible();
  await expect(page.getByText("Reported claim", { exact: true })).toBeVisible();
  await expect(page.getByText("Serving revision not observed")).toBeVisible();
  await expect(
    page.getByText("Verified evidence", { exact: true }),
  ).toHaveCount(0);
  // The refresh button names the source it will read.
  await page.getByRole("button", { name: "Check GitHub now" }).click();
  await expect(
    page.getByText(
      "Refresh queued. Evidence changes only after a reader reports back.",
    ),
  ).toBeVisible();
  expect(
    calls.filter((call) => call.method === "POST").map((call) => call.path),
  ).toEqual(["/work/card:release/refresh"]);
});

test("failed list reload keeps visible work and names stale display", async ({
  page,
}) => {
  let fail = false;
  await setup(page, {
    handle: async ({ path, method, reply }) => {
      if (fail && path === "/work" && method === "GET") {
        await reply(
          { error: { message: "Source projection unavailable" } },
          503,
        );
        return true;
      }
    },
  });
  await page.goto(`${root}/tasks`);
  await expect(page.locator("[data-work-ref]")).toHaveCount(3);
  fail = true;
  await page.getByRole("button", { name: "Reload", exact: true }).click();
  await expect(
    page.getByText("Showing the previously loaded records.", { exact: false }),
  ).toBeVisible();
  await expect(page.locator("[data-work-ref]")).toHaveCount(3);
});

test("PM retains failed draft and retries the same message intent without claiming completion", async ({
  page,
}) => {
  let attempts = 0;
  const { calls } = await setup(page, {
    handle: async ({ path, reply }) => {
      if (path.endsWith("/messages") && attempts++ === 0) {
        await reply({ error: { message: "PM bridge unavailable" } }, 503);
        return true;
      }
    },
  });
  await page.goto(`${root}/pm?work_ref=card%3Arelease`);
  await page
    .getByLabel("Message PM", { exact: true })
    .fill("What is still unverified?");
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  await expect(page.getByLabel("Message PM", { exact: true })).toHaveValue(
    "What is still unverified?",
  );
  await expect(page.getByRole("alert")).toContainText("PM bridge unavailable");
  await page.getByRole("button", { name: "Send message", exact: true }).click();
  // A queued turn is a transient state, so it reads as a live "Thinking · <elapsed>"
  // row rather than the durable badge this page used to show.
  await expect(page.getByText(/^Thinking/)).toBeVisible();
  const sends = calls.filter((call) => call.path.endsWith("/messages"));
  expect(sends).toHaveLength(2);
  expect(sends[0].body.request_key).toBe(sends[1].body.request_key);
  expect(
    calls.filter(
      (call) => call.path === "/pm/conversations" && call.method === "POST",
    ),
  ).toHaveLength(1);
});

test("answer and failed delivery remain separately inspectable", async ({
  page,
}) => {
  const { calls } = await setup(page, {
    handle: async ({ path, reply, actions }) => {
      if (path.endsWith("/dispatch")) {
        actions[0] = {
          ...actions[0],
          status: "failed",
          receipt: { detail: "Source delivery failed" },
        };
        await reply({ error: { message: "Source delivery failed" } }, 503);
        return true;
      }
    },
  });
  await page.goto(`${root}/inbox?item=decision:decision-sample`);
  await page.getByLabel("Authorize this scope").check();
  await page
    .getByLabel("Exact response")
    .fill("Approved for the sample note only");
  await page.getByRole("button", { name: "Record decision" }).click();
  await expect(
    page.getByText("Pending delivery", { exact: true }),
  ).toBeVisible();
  expect(calls.filter((call) => call.path.endsWith("/dispatch"))).toHaveLength(
    0,
  );
  await page
    .getByRole("button", { name: "Deliver approved instruction" })
    .click();
  await expect(page.getByText("Failed", { exact: true })).toBeVisible();
  await expect(
    page.getByText("Approved for the sample note only", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Outcome verified", { exact: true })).toHaveCount(
    0,
  );
});

for (const viewport of [
  { width: 390, height: 844 },
  { width: 1440, height: 1000 },
]) {
  test(`work and decision surfaces are accessible at ${viewport.width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport);
    await setup(page);
    for (const route of [
      { path: "/tasks", name: "tasks" },
      {
        path: "/inbox?item=decision:decision-sample",
        name: "inbox",
      },
      { path: "/pm", name: "pm" },
      { path: "/docs", name: "docs" },
      { path: "/docs/release-runbook", name: "docs-detail" },
      { path: "/integrations", name: "integrations" },
    ]) {
      await page.goto(`${root}${route.path}`);
      await expect(page.locator("h1").first()).toBeVisible();
      await expect(page.getByText("Loading tasks…")).toHaveCount(0);
      await expect(
        page.getByRole("button", { name: /Loading health|Loading decisions/ }),
      ).toHaveCount(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth + 1,
        ),
      ).toBe(true);
      const violations = (
        await new AxeBuilder({ page })
          .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
          .analyze()
      ).violations;
      expect(violations).toEqual([]);
      await page.screenshot({
        path: testInfo.outputPath(`${route.name}-${viewport.width}.png`),
        fullPage: true,
      });
    }
  });
}
