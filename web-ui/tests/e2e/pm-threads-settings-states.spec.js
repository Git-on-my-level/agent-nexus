import { expect as baseExpect, test } from "@playwright/test";

import {
  deferred,
  expectNoClippedContent,
  installWorkspaceApi,
} from "../helpers/workspaceApiMock.js";
import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

// The dev server compiles routes on demand; first paint of a route can take
// several seconds under parallel runs.
const expect = baseExpect.configure({ timeout: 20_000 });
test.describe.configure({ timeout: 120_000 });

/**
 * Walks Ask PM and the backing-thread surfaces through every UI state they can
 * reach (loading, empty, populated, in-flight, each error, popovers, modals,
 * long/ugly content) at several viewport sizes, auditing layout geometry after
 * every transition.
 */

const ROOT = "/o/local/w/local";
const LONG_ID = "pm_1fb951be68b4aa395611181d1d7af408";
const LONG_TOKEN =
  "unbroken_0123456789abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOP";
const LONG_SENTENCE =
  "The reader asked for the full reconciliation history of the vendor handoff, including every source revision we could not verify. ";

const minutesAgo = (minutes) =>
  new Date(Date.now() - minutes * 60_000).toISOString();

function conversation(index, title) {
  return {
    id: `conversation-${index}`,
    title,
    created_at: minutesAgo(index * 90),
  };
}

const LONG_TITLE_CONVERSATIONS = [
  conversation(1, "What needs my decision?"),
  conversation(
    2,
    `Reconcile ${LONG_TOKEN} against the vendor board and tell me who acts next`,
  ),
  conversation(3, LONG_TOKEN),
];

/** An answered turn with markdown, evidence refs and a proposed decision. */
const ANSWERED_TURN = {
  id: "turn-answered",
  text: `Why is ${LONG_TOKEN} still blocked? ${LONG_SENTENCE}`,
  created_at: minutesAgo(90),
  status: "delivered",
  response: [
    "Two commitments are blocked and one needs you.",
    "",
    "- `card:release` is waiting on a reviewer, last observed 3h ago.",
    `- The vendor delivery has no reader: \`${LONG_TOKEN}\` is the only token on file.`,
    "",
    `I proposed ${LONG_ID} for the handoff note, and decision:decision-unavailable for the rollback window.`,
    "",
    `${LONG_SENTENCE}${LONG_SENTENCE}`,
  ].join("\n"),
  evidence_refs: [
    "card:release",
    "card:a-very-long-card-handle-that-keeps-going-and-going-0123456789",
    "document:release-runbook",
    `thread:${LONG_TOKEN}`,
    "decision:decision-unavailable",
  ],
};

const DECISION_RECORDS = {
  [LONG_ID]: {
    id: LONG_ID,
    work_ref: "card:release",
    instruction: `Producer decision: update the sample handoff note. ${LONG_SENTENCE}`,
    status: "awaiting_answer",
    revision: 2,
    created_at: minutesAgo(80),
  },
};

const bothEnds = { scrollPositions: ["top", "bottom"] };

const THREAD_ID = "thread-vendor-reconciliation";

function threadRow(overrides) {
  return {
    id: "thread-onboarding",
    ref: "thread:onboarding",
    handle: "onboarding",
    title: "Customer onboarding workflow",
    topic_ref: "topic:thread-onboarding",
    state: "active",
    status: "active",
    type: "process",
    updated_at: minutesAgo(45),
    updated_by: "actor-operator",
    ...overrides,
  };
}

const THREAD_ROWS = [
  threadRow({}),
  threadRow({
    id: THREAD_ID,
    ref: `thread:${LONG_TOKEN}`,
    handle: LONG_TOKEN,
    title: `Vendor reconciliation ${LONG_TOKEN}`,
    topic_ref: `topic:${LONG_TOKEN}`,
  }),
  threadRow({
    id: "thread-archived",
    ref: "",
    handle: "",
    title: "",
    topic_ref: "",
    state: "archived",
  }),
];

function messageEvent(overrides) {
  return {
    id: "evt-1",
    ts: minutesAgo(30),
    type: "message_posted",
    actor_id: "actor-operator",
    thread_id: THREAD_ID,
    refs: [`thread:${THREAD_ID}`],
    summary: "Message",
    payload: { text: "Message" },
    ...overrides,
  };
}

const THREAD_TIMELINE = [
  messageEvent({
    id: "evt-long",
    ts: minutesAgo(90),
    summary: `Message: ${LONG_TOKEN}`,
    payload: { text: `${LONG_TOKEN} ${LONG_SENTENCE.repeat(2)}` },
  }),
  messageEvent({
    id: "evt-reply",
    ts: minutesAgo(60),
    actor_id: "actor-hermes",
    summary: "Message: reply",
    payload: {
      text: `Replying with the reconciliation detail. ${LONG_SENTENCE}`,
      reply_to_event_id: "evt-long",
    },
  }),
  messageEvent({
    id: "evt-short",
    ts: minutesAgo(10),
    summary: "Message: ok",
    payload: { text: "ok" },
  }),
];

const PRINCIPALS = [
  {
    agent_id: "agent-hermes",
    actor_id: "actor-hermes",
    username: "m4-hermes",
    principal_kind: "agent",
    auth_method: "public_key",
    revoked: false,
    registration: {
      handle: "m4-hermes",
      actor_id: "actor-hermes",
      status: "active",
      workspace_bindings: [{ workspace_id: "local", enabled: true }],
    },
    wake_routing: {
      applicable: true,
      handle: "m4-hermes",
      taggable: true,
      online: true,
      state: "online",
      summary: "Online as @m4-hermes.",
    },
  },
  {
    agent_id: "agent-long",
    actor_id: "actor-long",
    username: `agent-with-a-very-long-handle-${LONG_TOKEN}`,
    principal_kind: "agent",
    auth_method: "public_key",
    revoked: false,
    registration: {
      handle: `agent-with-a-very-long-handle-${LONG_TOKEN}`,
      actor_id: "actor-long",
      status: "active",
      workspace_bindings: [{ workspace_id: "local", enabled: true }],
    },
    wake_routing: {
      applicable: true,
      handle: `agent-with-a-very-long-handle-${LONG_TOKEN}`,
      taggable: true,
      online: false,
      state: "offline",
      summary: `Offline. The bridge has not checked in. ${LONG_SENTENCE}`,
    },
  },
];

/** The PM thread is its own scroll container; the window barely moves. */
async function scrollThread(page, position) {
  await page.evaluate((where) => {
    const thread = document.querySelector(".pm-thread");
    if (!thread) return;
    thread.scrollTop = where === "top" ? 0 : thread.scrollHeight;
    thread.dispatchEvent(new Event("scroll"));
  }, position);
  await page.waitForTimeout(80);
}

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`ask pm states @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
    });

    test("empty thread, starters, long draft and history popover", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page, {
        conversations: LONG_TITLE_CONVERSATIONS,
        conversationsCursor: "cursor-2",
      });
      await page.goto(`${ROOT}/pm`);
      await expect(page.getByRole("heading", { name: "Ask PM" })).toBeVisible();
      await expect(page.getByText("Nothing asked yet.")).toBeVisible();
      await expectCleanLayout(page, "empty thread", bothEnds);

      await page
        .getByRole("button", { name: "What needs my decision?" })
        .click();
      await expect(page.locator("#pm-message")).toHaveValue(
        "What needs my decision?",
      );
      await expectCleanLayout(page, "starter chosen");

      // A pasted wall of text grows the composer up to its cap.
      await page
        .locator("#pm-message")
        .fill(`${LONG_TOKEN} ${LONG_SENTENCE.repeat(6)}`);
      await expectCleanLayout(page, "long draft");
      await page.locator("#pm-message").fill("");

      await page.locator(".pm-history > summary").click();
      await expect(
        page.getByRole("navigation", { name: "Conversation history" }),
      ).toBeVisible();
      await expectCleanLayout(page, "history open");

      api.hold.conversations = deferred();
      await page.getByRole("button", { name: "More conversations" }).click();
      await expect(page.getByText("Loading…", { exact: true })).toBeVisible();
      await expectCleanLayout(page, "history loading more");
      api.conversationsCursor = "";
      api.conversationsHasMore = true;
      api.hold.conversations.resolve();
      api.hold = {};
      await expect(page.getByText("Partial history")).toBeVisible();
      await expectCleanLayout(page, "history partial");

      // Deep link from a task: the context line shows the ref with no thread.
      await page.goto(
        `${ROOT}/pm?new=1&work_ref=${encodeURIComponent(`card:${LONG_TOKEN}`)}`,
      );
      await expect(
        page.getByRole("link", { name: "Open the task" }),
      ).toBeVisible();
      await expectCleanLayout(page, "new conversation for a task", bothEnds);
      await expectNoClippedContent(page, "new conversation for a task");
    });

    test("populated conversation with evidence and proposals", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page, {
        conversations: [
          {
            ...conversation(1, `Vendor reconciliation ${LONG_TOKEN}`),
            work_ref: "card:a-very-long-card-handle-0123456789abcdef",
          },
        ],
        turns: [
          {
            id: "turn-first",
            text: "What changed since I last checked?",
            created_at: minutesAgo(200),
            status: "delivered",
            response: "Nothing changed while you were away.",
          },
          // Enough history that the thread scrolls at every viewport.
          ...Array.from({ length: 6 }, (_, index) => ({
            id: `turn-filler-${index}`,
            text: `Filler question ${index}. ${LONG_SENTENCE}`,
            created_at: minutesAgo(180 - index * 10),
            status: "delivered",
            response: `Filler answer ${index}. ${LONG_SENTENCE}`,
          })),
          ANSWERED_TURN,
        ],
        turnsCursor: "cursor-older",
        olderTurns: [
          {
            id: "turn-older",
            text: LONG_TOKEN,
            created_at: minutesAgo(400),
            status: "delivered",
            response: `Older answer. ${LONG_SENTENCE}`,
          },
        ],
        decisions: DECISION_RECORDS,
        work: [
          {
            id: "release",
            ref: "card:release",
            handle: "release",
            title: "Release the sample workspace",
          },
        ],
      });
      await page.goto(`${ROOT}/pm?conversation=conversation-1`);
      await expect(
        page.getByText("Two commitments are blocked", { exact: false }),
      ).toBeVisible();
      await expect(
        page.getByRole("list", { name: "Decisions proposed in this reply" }),
      ).toBeVisible();
      await expect(
        page.getByText("Unavailable", { exact: true }),
      ).toBeVisible();
      await expectCleanLayout(page, "populated conversation", bothEnds);
      await expectNoClippedContent(page, "populated conversation");

      // Scrolled off the newest turn: the jump affordance floats over the thread.
      await scrollThread(page, "top");
      await expect(
        page.getByRole("button", { name: "Jump to latest ↓" }),
      ).toBeVisible();
      await expectCleanLayout(page, "jump to latest visible");

      api.hold.olderTurns = deferred();
      await page.getByRole("button", { name: "Older messages" }).click();
      await expect(page.getByText("Loading older messages…")).toBeVisible();
      await expectCleanLayout(page, "loading older messages");
      api.hold.olderTurns.resolve();
      api.hold = {};
      await expect(
        page.getByText("Older answer.", { exact: false }),
      ).toBeVisible();
      await expectCleanLayout(page, "older messages loaded");

      await page.getByRole("button", { name: "Jump to latest ↓" }).click();
      await expectCleanLayout(page, "jumped to latest");
    });

    test("queued, long-waiting, stalled and failed turns", async ({ page }) => {
      await installWorkspaceApi(page, {
        conversations: [conversation(1, "Pending work")],
        turns: [
          {
            id: "turn-queued",
            text: "Which commitments are blocked, and who acts next?",
            created_at: minutesAgo(0),
            status: "queued",
            claimed: false,
          },
          {
            id: "turn-stalled",
            text: LONG_TOKEN,
            created_at: minutesAgo(12),
            status: "in_progress",
            claimed: true,
          },
          {
            id: "turn-failed",
            text: "Where is the evidence still uncertain?",
            created_at: minutesAgo(30),
            status: "failed",
            failure: `The PM runner refused this turn. ${LONG_SENTENCE}${LONG_TOKEN}`,
          },
          {
            id: "turn-unknown",
            text: "Did that land?",
            created_at: minutesAgo(25),
            status: "unknown",
          },
        ],
      });
      await page.goto(`${ROOT}/pm?conversation=conversation-1`);
      await expect(page.getByText("Queued", { exact: true })).toBeVisible();
      await expect(
        page.getByText("No reply yet — the PM runner may be off."),
      ).toBeVisible();
      await expect(page.getByText("Delivery uncertain")).toBeVisible();
      await expectCleanLayout(page, "pending and failed turns", bothEnds);
    });

    test("send refusals, retry and an expired session", async ({ page }) => {
      const api = await installWorkspaceApi(page, {
        conversations: [conversation(1, "Capacity")],
        turns: [
          {
            id: "turn-answered-1",
            text: "What needs my decision?",
            created_at: minutesAgo(20),
            status: "delivered",
            response: "Nothing right now.",
          },
        ],
      });
      await page.goto(`${ROOT}/pm?conversation=conversation-1`);
      await expect(page.getByText("Nothing right now.")).toBeVisible();

      const composer = page.locator("#pm-message");
      await composer.fill(`Retry the vendor handoff ${LONG_TOKEN}`);
      api.hold.sendMessage = deferred();
      await page.getByRole("button", { name: "Send message" }).click();
      await expectCleanLayout(page, "sending");
      api.hold.sendMessage.resolve();
      api.hold = {};

      // Core refuses: the workspace PM queue is full.
      api.fail.sendMessage = {
        status: 429,
        body: {
          error: {
            code: "busy",
            message: "capacity reached",
            details: { reason: "queue", limit: 20 },
          },
        },
      };
      await composer.fill(`Ask again ${LONG_TOKEN}`);
      await page.getByRole("button", { name: "Send message" }).click();
      await expect(page.getByRole("alert")).toContainText("PM queue");
      await expectCleanLayout(page, "send refused", bothEnds);

      await expect(
        page.getByRole("button", { name: "Send again" }),
      ).toBeVisible();
      await page.getByRole("button", { name: "Send again" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "send refused twice");

      // History can be open over the refusal banner.
      await page.locator(".pm-history > summary").click();
      await expect(
        page.getByRole("navigation", { name: "Conversation history" }),
      ).toBeVisible();
      await expectCleanLayout(
        page,
        "history over the refusal banner",
        bothEnds,
      );
      await page.locator(".pm-history > summary").click();

      // An expired session stops the thread and offers sign-in.
      api.fail = {};
      await composer.fill("");
      api.fail.conversation = {
        status: 401,
        body: {
          error: {
            code: "invalid_token",
            message: "token is invalid, expired, or revoked",
          },
        },
      };
      await page.goto(`${ROOT}/pm?conversation=conversation-1`);
      await expect(
        page.getByRole("button", { name: "Sign in again" }),
      ).toBeVisible();
      await expectCleanLayout(page, "session expired", bothEnds);
    });

    test("unreachable PM disables sending", async ({ page }) => {
      const api = await installWorkspaceApi(page, {
        conversations: LONG_TITLE_CONVERSATIONS,
      });
      api.fail.conversations = {
        message: `The PM service is unreachable from this workspace. ${LONG_SENTENCE}`,
      };
      api.hold.conversations = deferred();
      await page.goto(`${ROOT}/pm`);
      await expect(page.getByText("Loading…", { exact: true })).toBeVisible();
      await expectCleanLayout(page, "initial loading");

      api.hold.conversations.resolve();
      api.hold = {};
      await expect(
        page.getByText("Sending is disabled until PM is reachable."),
      ).toBeVisible();
      await expectCleanLayout(page, "pm unreachable", bothEnds);

      api.fail = {};
      await page.getByRole("button", { name: "Retry" }).click();
      await expect(page.getByRole("alert")).toHaveCount(0);
      await expectCleanLayout(page, "recovered after retry");
    });
  });

  test.describe(`thread states @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
    });

    test("thread list: loading, populated, filters, empty and failed", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page, { threads: THREAD_ROWS });
      api.hold.threads = deferred();
      await page.goto(`${ROOT}/threads`);
      await expect(
        page.getByRole("heading", { name: "Threads" }),
      ).toBeVisible();
      await expectCleanLayout(page, "thread list loading");

      api.hold.threads.resolve();
      api.hold = {};
      await expect(
        page.getByText("Customer onboarding workflow"),
      ).toBeVisible();
      await expectCleanLayout(page, "thread list populated", bothEnds);
      await expectNoClippedContent(page, "thread list populated");

      await page.getByTestId("topics-filters-toggle").click();
      await expect(page.getByTestId("topics-filter-panel")).toBeVisible();
      await expectCleanLayout(page, "thread filters open", bothEnds);

      await page.getByPlaceholder("Title or id…").fill(LONG_TOKEN);
      api.threads = [];
      await page.getByRole("button", { name: "Apply" }).click();
      await expect(
        page.getByText("No threads match the current filters"),
      ).toBeVisible();
      await expectCleanLayout(page, "thread list filtered empty", bothEnds);

      api.fail.threads = {
        message: `backing thread index unavailable. ${LONG_SENTENCE}${LONG_TOKEN}`,
      };
      await page
        .getByTestId("topics-filter-panel")
        .getByRole("button", { name: "Clear filters" })
        .click();
      await expect(page.getByText(/backing thread index/)).toBeVisible();
      await expectCleanLayout(page, "thread list failed", bothEnds);

      // Retry clears the alert and reloads in place.
      api.fail = {};
      api.threads = THREAD_ROWS;
      await page.getByRole("button", { name: "Retry" }).click();
      await expect(
        page.getByText("Customer onboarding workflow"),
      ).toBeVisible();
      await expectCleanLayout(page, "thread list recovered", bothEnds);
    });

    test("thread detail: loading, failure and missing thread", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page, {
        principals: PRINCIPALS,
        topic: null,
      });
      api.hold.threadWorkspace = deferred();
      await page.goto(`${ROOT}/threads/${THREAD_ID}`);
      await expect(page.getByText("Loading...")).toBeVisible();
      await expectCleanLayout(page, "thread detail loading");

      api.hold.threadWorkspace.resolve();
      api.hold = {};
      await expect(page.getByText("Thread not found.")).toBeVisible();
      await expectCleanLayout(page, "thread not found");

      api.fail.threadWorkspace = {
        message: `thread workspace projection failed. ${LONG_SENTENCE}${LONG_TOKEN}`,
      };
      await page.reload();
      await expect(
        page.getByText(/thread workspace projection failed/),
      ).toBeVisible();
      await expectCleanLayout(page, "thread detail failed", bothEnds);
    });

    test("thread detail messages: long text, mentions, reply and post error", async ({
      page,
    }) => {
      const api = await installWorkspaceApi(page, {
        principals: PRINCIPALS,
        actors: [
          { id: "actor-operator", display_name: "Operator", tags: ["human"] },
          { id: "actor-hermes", display_name: "m4-hermes" },
        ],
        topic: {
          id: THREAD_ID,
          type: "process",
          title: `Vendor reconciliation ${LONG_TOKEN}`,
          status: "active",
          current_summary: `${LONG_SENTENCE}${LONG_TOKEN}`,
          next_actions: ["Collect legal signoff"],
          open_cards: [],
          updated_at: minutesAgo(20),
          updated_by: "actor-operator",
        },
        timeline: THREAD_TIMELINE,
      });
      await page.goto(`${ROOT}/threads/${THREAD_ID}?tab=messages`);
      await expect(page.locator("#message-evt-short")).toContainText("ok");
      await expectCleanLayout(page, "thread messages", bothEnds);
      await expectNoClippedContent(page, "thread messages");

      const composer = page.locator("#message-text");
      await composer.fill("@");
      await expect(page.locator("#message-mention-list")).toBeVisible();
      await expectCleanLayout(page, "mention list open");
      await page.keyboard.press("Escape");

      await page
        .locator("#message-evt-long")
        .getByRole("button", { name: "Reply" })
        .first()
        .click({ force: true });
      // The reply target seeds the composer asynchronously; fill after it lands.
      await expect(page.getByText("Replying to")).toBeVisible();
      await composer.fill(`${LONG_TOKEN} ${LONG_SENTENCE}`);
      await expectCleanLayout(page, "reply chip and long draft", bothEnds);

      // An attachment chip sits between the hint and the actions.
      api.attachmentName = `evidence-${LONG_TOKEN}.txt`;
      api.attachmentId = "artifact-upload-1";
      await page.locator('input[type="file"]').setInputFiles({
        name: api.attachmentName,
        mimeType: "text/plain",
        buffer: Buffer.from("attachment body"),
      });
      await expect(page.getByText("Attached")).toBeVisible();
      await expectCleanLayout(page, "attachment pending", bothEnds);

      api.fail.postEvent = {
        message: `message rejected by core. ${LONG_SENTENCE}${LONG_TOKEN}`,
      };
      const send = page.getByRole("button", { name: /^(Send|Post)/ });
      await expect(async () => {
        await composer.fill(`${LONG_TOKEN} ${LONG_SENTENCE}`);
        await baseExpect(send).toBeEnabled({ timeout: 2_000 });
      }).toPass({ timeout: 30_000 });
      await send.click();
      await expect(page.getByText(/message rejected by core/)).toBeVisible();
      await expectCleanLayout(page, "post message failed", bothEnds);
    });

    test("thread detail: about, docs and timeline tabs", async ({ page }) => {
      await installWorkspaceApi(page, {
        principals: PRINCIPALS,
        topic: {
          id: THREAD_ID,
          type: "process",
          title: `Vendor reconciliation ${LONG_TOKEN}`,
          status: "active",
          current_summary: `${LONG_SENTENCE.repeat(2)}${LONG_TOKEN}`,
          next_actions: [`Collect legal signoff for ${LONG_TOKEN}`],
          open_cards: ["card-onboard-1"],
          key_artifacts: [`artifact:${LONG_TOKEN}`],
          updated_at: minutesAgo(20),
          updated_by: "actor-operator",
        },
        documents: [
          {
            id: "doc-runbook",
            title: `Release runbook ${LONG_TOKEN}`,
            status: "active",
            updated_at: minutesAgo(120),
            updated_by: "actor-operator",
            head_revision_id: "rev-runbook-2",
            head_revision_number: 2,
            head_revision: {
              revision_id: "rev-runbook-2",
              revision_number: 2,
              content_type: "text",
              created_at: minutesAgo(120),
            },
          },
        ],
        timeline: THREAD_TIMELINE,
      });
      await page.goto(`${ROOT}/threads/${THREAD_ID}?tab=about`);
      await expect(page.getByRole("tab", { name: "About" })).toHaveAttribute(
        "aria-selected",
        "true",
      );
      await expectCleanLayout(page, "about tab", bothEnds);
      await expectNoClippedContent(page, "about tab");

      await page.getByRole("tab", { name: "Docs" }).click();
      await expect(
        page.getByRole("link", { name: /Release runbook/ }),
      ).toBeVisible();
      await expectCleanLayout(page, "docs tab", bothEnds);

      await page.getByRole("tab", { name: "Timeline" }).click();
      await expect(page.locator("#event-evt-short")).toBeVisible();
      await expectCleanLayout(page, "timeline tab", bothEnds);
    });

    test("thread detail: archived and trashed notices", async ({ page }) => {
      const api = await installWorkspaceApi(page, {
        principals: PRINCIPALS,
        topic: {
          id: THREAD_ID,
          type: "process",
          title: `Vendor reconciliation ${LONG_TOKEN}`,
          status: "active",
          current_summary: "Archived while the vendor reader is down.",
          updated_at: minutesAgo(20),
          updated_by: "actor-operator",
          archived_at: minutesAgo(200),
          archived_by: "actor-operator",
        },
        timeline: THREAD_TIMELINE,
      });
      await page.goto(`${ROOT}/threads/${THREAD_ID}?tab=messages`);
      // No "on": formatTimestamp is relative under 7 days, so the old copy
      // read "was archived on 3h ago". See archived-copy-states.spec.js.
      await expect(page.getByText(/was archived 3h ago/)).toBeVisible();
      await expectCleanLayout(page, "archived thread notice", bothEnds);

      api.topic = {
        ...api.topic,
        archived_at: "",
        trashed_at: minutesAgo(100),
        trashed_by: "actor-operator",
        trash_reason: `Superseded by the new vendor board. ${LONG_SENTENCE}`,
      };
      await page.reload();
      await expect(page.getByText("This topic is in trash")).toBeVisible();
      await expectCleanLayout(page, "trashed thread notice", bothEnds);
      await expectNoClippedContent(page, "trashed thread notice");
    });
  });
}
