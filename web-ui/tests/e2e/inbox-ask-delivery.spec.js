import { expect, test } from "@playwright/test";

/**
 * Ask delivery in the Inbox: the Evidence panel, "I need more context", the
 * delivery state Handled shows for an answered item, and the Stale fold.
 *
 * Every core read is mocked, so these assert what the UI does with the
 * contract's `AskOutcome`, the ask event's authoring payload and the inbox
 * projection — not what a live core happens to hold.
 */

const ACTOR_ID = "actor-e2e";

function corePathname(urlLike) {
  const url = typeof urlLike === "string" ? new URL(urlLike) : urlLike;
  const raw =
    url.pathname.endsWith("/") && url.pathname.length > 1
      ? url.pathname.slice(0, -1)
      : url.pathname;
  return raw || "/";
}

/** Browser GET inbox.list resolves to the proxied core pathname `/inbox`. */
function isInboxListUrl(urlLike) {
  const path = corePathname(urlLike);
  if (/^\/o\/[^/]+\/w\/[^/]+\/inbox(?:\/|$)/.test(path)) return false;
  if (/^\/ws\/[^/]+\/[^/]+\/inbox(?:\/|$)/.test(path)) return false;
  return path === "/inbox";
}

function singleSegment(urlLike, prefix) {
  const path = corePathname(urlLike);
  if (!path.startsWith(prefix)) return "";
  const rest = path.slice(prefix.length);
  if (!rest || rest.includes("/")) return "";
  try {
    return decodeURIComponent(rest);
  } catch {
    return rest;
  }
}

const isAskUrl = (url) => Boolean(singleSegment(url, "/asks/"));
const isEventUrl = (url) => Boolean(singleSegment(url, "/events/"));
const isDocUrl = (url) => Boolean(singleSegment(url, "/docs/"));
const isInboxItemUrl = (url) => {
  const id = singleSegment(url, "/inbox/");
  return Boolean(id) && id !== "stream" && !id.startsWith("__");
};
function isRespondUrl(urlLike) {
  const path = corePathname(urlLike);
  if (!path.startsWith("/inbox/") || !path.endsWith("/respond")) return false;
  const middle = path.slice("/inbox/".length, path.length - "/respond".length);
  return Boolean(middle) && !middle.includes("/");
}

function respondedItemId(urlLike) {
  const path = corePathname(urlLike);
  const middle = path.slice("/inbox/".length, path.length - "/respond".length);
  try {
    return decodeURIComponent(middle);
  } catch {
    return middle;
  }
}

const EVIDENCE_ASK = {
  id: "inbox-evidence",
  kind: "ask",
  title: "Pick a rulings rollout",
  body: 'Proceed per doc "rulings"? PR #123 carries the change.',
  subject_ref: "card:anx-7",
  thread_id: "thread-rollout",
  related_refs: ["thread:thread-rollout", "document:rulings"],
  request_event_ref: "event:ask-evidence",
  source_event_id: "ask-evidence",
  response_proposals: ["Proceed.", "Hold until Monday."],
  source_event_time: new Date(Date.now() - 3 * 3_600_000).toISOString(),
};

const STALE_ASK = {
  id: "inbox-stale",
  kind: "ask",
  title: "Old question nobody closed",
  body: "Still open.",
  subject_ref: "card:anx-9",
  thread_id: "thread-old",
  related_refs: ["thread:thread-old"],
  request_event_ref: "event:ask-stale",
  source_event_id: "ask-stale",
  response_proposals: ["Proceed."],
  is_stale: true,
  source_event_time: new Date(Date.now() - 400 * 3_600_000).toISOString(),
};

const REASK = {
  id: "inbox-reask",
  kind: "ask",
  title: "Re-asked with the evidence",
  body: "Now with the rulings doc attached.",
  subject_ref: "card:anx-7",
  thread_id: "thread-rollout",
  related_refs: ["thread:thread-rollout"],
  request_event_ref: "event:ask-reask",
  source_event_id: "ask-reask",
  response_proposals: ["Proceed."],
  source_event_time: new Date(Date.now() - 3_600_000).toISOString(),
};

const SENT_BACK = {
  id: "completed:resp-ctx",
  status: "completed",
  inbox_item_id: "inbox-ctx",
  kind: "ask",
  title: "Earlier question",
  thread_id: "thread-rollout",
  subject_ref: "card:anx-7",
  related_refs: ["thread:thread-rollout"],
  request_event_ref: "event:ask-ctx",
  response_event_ref: "event:resp-ctx",
  response_text: "Which rollout window do you mean?",
  outcome: "needs_context",
  responded_at: new Date(Date.now() - 2 * 3_600_000).toISOString(),
  responding_actor_id: ACTOR_ID,
};

const DELIVERED = {
  id: "completed:resp-del",
  status: "completed",
  inbox_item_id: "inbox-del",
  kind: "ask",
  title: "Rollout approved",
  thread_id: "thread-rollout",
  subject_ref: "card:anx-7",
  related_refs: ["thread:thread-rollout"],
  request_event_ref: "event:ask-del",
  response_event_ref: "event:resp-del",
  response_text: "Proceed.",
  outcome: "answered",
  responded_at: new Date(Date.now() - 3_600_000).toISOString(),
  responding_actor_id: ACTOR_ID,
};

const NO_SUBSCRIBER = {
  id: "completed:resp-none",
  status: "completed",
  inbox_item_id: "inbox-none",
  kind: "ask",
  title: "Answered with nobody listening",
  thread_id: "thread-rollout",
  subject_ref: "card:anx-7",
  related_refs: ["thread:thread-rollout"],
  request_event_ref: "event:ask-none",
  response_event_ref: "event:resp-none",
  response_text: "Proceed.",
  outcome: "answered",
  responded_at: new Date(Date.now() - 1_800_000).toISOString(),
  responding_actor_id: ACTOR_ID,
};

const ASK_OUTCOMES = {
  "event:ask-evidence": {
    ask_id: "event:ask-evidence",
    status: "open",
    subject_ref: "card:anx-7",
    is_stale: false,
    delivery: [],
  },
  "event:ask-stale": {
    ask_id: "event:ask-stale",
    status: "open",
    subject_ref: "card:anx-9",
    is_stale: true,
    delivery: [],
  },
  "event:ask-reask": {
    ask_id: "event:ask-reask",
    status: "open",
    subject_ref: "card:anx-7",
    is_stale: false,
    delivery: [],
  },
  "event:ask-ctx": {
    ask_id: "event:ask-ctx",
    status: "needs_context",
    subject_ref: "card:anx-7",
    is_stale: false,
    task_outcome: {
      card_ref: "card:anx-7",
      phase: "ready",
      next_actor: "codex-worker",
    },
    delivery: [],
  },
  "event:ask-del": {
    ask_id: "event:ask-del",
    status: "answered",
    subject_ref: "card:anx-7",
    is_stale: false,
    task_outcome: {
      card_ref: "card:anx-7",
      phase: "ready",
      next_actor: "codex-worker",
    },
    delivery: [
      {
        id: "sub_await",
        kind: "await",
        label: "cli await",
        state: "delivered",
        attempts: 1,
        last_at: new Date(Date.now() - 3_000_000).toISOString(),
      },
      {
        id: "sub_hook",
        kind: "webhook",
        label: "release notifier",
        state: "failed",
        attempts: 5,
        reason: "502 from endpoint",
        last_at: new Date(Date.now() - 2_000_000).toISOString(),
      },
    ],
  },
  "event:ask-none": {
    ask_id: "event:ask-none",
    status: "answered",
    subject_ref: "card:anx-7",
    is_stale: false,
    task_outcome: {
      card_ref: "card:anx-7",
      phase: "ready",
      next_actor: "codex-worker",
    },
    delivery: [],
  },
};

const ASK_EVENTS = {
  "ask-evidence": {
    id: "ask-evidence",
    type: "human_attention_requested",
    thread_id: "thread-rollout",
    payload: {
      kind: "ask",
      subject_ref: "card:anx-7",
      related_refs: ["document:rulings"],
      evidence: [
        { label: "Rollout change", url: "https://example.org/repo/pull/123" },
      ],
    },
  },
  "ask-reask": {
    id: "ask-reask",
    type: "human_attention_requested",
    thread_id: "thread-rollout",
    payload: {
      kind: "ask",
      subject_ref: "card:anx-7",
      supersedes: "event:ask-ctx",
    },
  },
};

async function setupInbox(page, { open = [], completed = [] } = {}) {
  const state = { respondBodies: [], openItems: [...open] };

  await page.addInitScript((actorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", actorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, ACTOR_ID);

  const json = (route, body, status = 200) =>
    route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(body),
    });

  await page.route(/\/actors(?:\?.*)?$/, (route) =>
    json(route, {
      actors: [{ id: ACTOR_ID, display_name: "E2E User", tags: ["human"] }],
    }),
  );
  for (const [pattern, body] of [
    [/\/pm\/decisions(?:\?.*)?$/, { items: [] }],
    [/\/pm\/actions(?:\?.*)?$/, { items: [] }],
    [/\/work(?:\?.*)?$/, { work: [] }],
    [/\/home\/unread(?:\?.*)?$/, { groups: [] }],
    [/\/events(?:\?.*)?$/, { events: [] }],
  ]) {
    await page.route(pattern, (route) => json(route, body));
  }
  for (const pattern of [
    /\/stream\/events(?:\?.*)?$/,
    /\/stream\/inbox(?:\?.*)?$/,
  ]) {
    await page.route(pattern, (route) =>
      route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "",
      }),
    );
  }

  await page.route(isEventUrl, (route, request) => {
    const id = singleSegment(request.url(), "/events/");
    const event = ASK_EVENTS[id];
    return event ? json(route, { event }) : json(route, { error: "nope" }, 404);
  });

  await page.route(isAskUrl, (route, request) => {
    const ref = singleSegment(request.url(), "/asks/");
    const outcome = ASK_OUTCOMES[ref];
    return outcome ? json(route, outcome) : json(route, { error: "nope" }, 404);
  });

  await page.route(isDocUrl, (route, request) => {
    const id = singleSegment(request.url(), "/docs/");
    if (id !== "rulings") return json(route, { error: "nope" }, 404);
    return json(route, {
      document: { id: "rulings", title: "Rulings", head_revision_number: 4 },
      revision: {
        content: "## Rulings\n\nThe window is Monday to Thursday.",
        content_type: "text",
      },
    });
  });

  await page.route(isRespondUrl, async (route, request) => {
    if (request.method() !== "POST") return route.continue();
    const id = respondedItemId(request.url());
    state.respondBodies.push({ id, body: request.postDataJSON() });
    state.openItems = state.openItems.filter((item) => item.id !== id);
    return json(
      route,
      {
        event: { id: "event-resp", type: "human_attention_responded" },
        task_outcome: {
          card_ref: "card:anx-7",
          phase: "ready",
          next_actor: "codex-worker",
        },
        notify: { requested: true, queued: true, mode: "original" },
      },
      201,
    );
  });

  await page.route(isInboxItemUrl, (route, request) => {
    if (request.method() !== "GET") return route.continue();
    const id = singleSegment(request.url(), "/inbox/");
    const item =
      state.openItems.find((entry) => entry.id === id) ||
      completed.find((entry) => entry.id === id);
    return item ? json(route, { item }) : json(route, { error: "nope" }, 404);
  });

  await page.route(isInboxListUrl, (route, request) => {
    if (request.method() !== "GET") return route.continue();
    const status = new URL(request.url()).searchParams.get("status") ?? "open";
    return json(route, {
      status,
      items: status === "completed" ? completed : state.openItems,
      generated_at: new Date().toISOString(),
    });
  });

  return state;
}

test("ask evidence links its refs and opens a document beside the question", async ({
  page,
}) => {
  await setupInbox(page, { open: [EVIDENCE_ASK] });
  await page.goto("/o/local/w/local/inbox");

  const row = page.getByTestId(`inbox-row-${EVIDENCE_ASK.id}`);
  await expect(row).toBeVisible();
  await row.click();

  const evidence = page.locator("[data-inbox-evidence]");
  await expect(evidence).toBeVisible();
  await expect(
    evidence.locator("[data-inbox-evidence-doc='document:rulings']"),
  ).toHaveText("rulings");
  const link = evidence.locator(
    "[data-inbox-evidence-link='https://example.org/repo/pull/123']",
  );
  await expect(link).toContainText("Rollout change");
  await expect(evidence).toContainText("Pull request");

  // The body's own names are links now, not prose the reader has to chase.
  const body = page.locator("[aria-label='Selected inbox item']");
  await expect(body.getByRole("link", { name: "rulings" })).toBeVisible();
  await expect(body.getByRole("link", { name: "PR #123" })).toHaveAttribute(
    "href",
    "https://example.org/repo/pull/123",
  );

  const before = page.url();
  await evidence
    .locator("[data-inbox-evidence-doc='document:rulings']")
    .click();
  const panel = page.locator("[data-inbox-doc-panel='document:rulings']");
  await expect(panel).toBeVisible();
  await expect(panel).toContainText("Monday to Thursday");
  await expect(panel.locator("[data-inbox-doc-panel-open]")).toHaveAttribute(
    "href",
    /\/docs\/rulings$/,
  );
  expect(page.url()).toBe(before);

  await panel.locator("[data-inbox-doc-panel-close]").click();
  await expect(panel).toHaveCount(0);
});

test("I need more context sends the ask back and Handled says so", async ({
  page,
}) => {
  const state = await setupInbox(page, {
    open: [EVIDENCE_ASK],
    completed: [SENT_BACK],
  });
  await page.goto("/o/local/w/local/inbox");

  await page.getByTestId(`inbox-row-${EVIDENCE_ASK.id}`).click();
  await page.locator("[data-inbox-needs-context-open]").click();
  await page
    .locator("[data-inbox-needs-context-note]")
    .fill("Which rollout window do you mean?");
  await page.locator("[data-inbox-needs-context-send]").click();

  await expect
    .poll(() => state.respondBodies.length, { timeout: 15_000 })
    .toBeGreaterThan(0);
  expect(state.respondBodies[0].id).toBe(EVIDENCE_ASK.id);
  expect(state.respondBodies[0].body.outcome).toBe("needs_context");
  expect(state.respondBodies[0].body.response_text).toBe(
    "Which rollout window do you mean?",
  );

  // It has left Needs you.
  await expect(page.getByTestId(`inbox-row-${EVIDENCE_ASK.id}`)).toHaveCount(0);

  await page.goto("/o/local/w/local/inbox?mailbox=handled");
  const handled = page.getByTestId(`inbox-row-${SENT_BACK.id}`);
  await expect(handled).toBeVisible();
  await expect(handled).toContainText("Sent back for context");
  await handled.click();
  await expect(page.locator("[data-inbox-sent-back]")).toBeVisible();
});

test("a re-asked question links the ask it supersedes", async ({ page }) => {
  await setupInbox(page, { open: [REASK], completed: [SENT_BACK] });
  await page.goto("/o/local/w/local/inbox");

  await page.getByTestId(`inbox-row-${REASK.id}`).click();
  const link = page.locator("[data-inbox-supersedes='event:ask-ctx']");
  await expect(link).toBeVisible();
  await expect(page.locator("text=Re-asked (supersedes")).toBeVisible();
  // The superseded ask is already loaded under Handled, so the link opens it
  // in the Inbox rather than sending the reader to the event log.
  await expect(link).toHaveAttribute("href", /item=completed%3Aresp-ctx/);
});

test("Handled names the task outcome and every delivery state", async ({
  page,
}) => {
  await setupInbox(page, { completed: [DELIVERED, NO_SUBSCRIBER] });
  await page.goto("/o/local/w/local/inbox?mailbox=handled");

  await page.getByTestId(`inbox-row-${DELIVERED.id}`).click();
  const delivery = page.locator("[data-inbox-delivery]");
  await expect(delivery).toBeVisible();
  await expect(delivery.locator("[data-inbox-task-outcome]")).toHaveText(
    "Unblocked · next: codex-worker",
  );
  const awaitRow = delivery.locator("[data-inbox-delivery-row='sub_await']");
  await expect(awaitRow).toContainText("Live await");
  await expect(awaitRow).toContainText("Delivered");
  const hookRow = delivery.locator("[data-inbox-delivery-row='sub_hook']");
  await expect(hookRow).toContainText("Webhook");
  await expect(hookRow).toContainText("Failed");
  await expect(hookRow).toContainText("502 from endpoint");
  await expect(hookRow).toContainText("5 attempts");
  // A failed delivery offers no retry: only the subscriber may record one.
  await expect(delivery.getByRole("button", { name: /retry/i })).toHaveCount(0);

  await page.getByTestId(`inbox-row-${NO_SUBSCRIBER.id}`).click();
  await expect(page.locator("[data-inbox-delivery-none]")).toHaveText(
    "Not delivered: no subscriber; task unblocked for codex-worker.",
  );
});

test("a stale ask folds into the Stale group", async ({ page }) => {
  await setupInbox(page, { open: [EVIDENCE_ASK, STALE_ASK] });
  await page.goto("/o/local/w/local/inbox");

  await expect(page.getByTestId(`inbox-row-${EVIDENCE_ASK.id}`)).toBeVisible();
  const stale = page.locator("[data-inbox-stale]");
  await expect(stale.getByRole("button", { name: "Stale (1)" })).toBeVisible();
  await expect(page.getByTestId(`inbox-row-${STALE_ASK.id}`)).toHaveCount(0);

  await stale.getByRole("button", { name: "Stale (1)" }).click();
  await expect(page.getByTestId(`inbox-row-${STALE_ASK.id}`)).toBeVisible();
  // A stale ask is not an archivable stale task; it offers no Archive button.
  await expect(stale.getByRole("button", { name: /^Archive / })).toHaveCount(0);
});

test("evidence reads as a sheet at phone width", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await setupInbox(page, { open: [EVIDENCE_ASK] });
  await page.goto("/o/local/w/local/inbox");

  await page.getByTestId(`inbox-row-${EVIDENCE_ASK.id}`).click();
  await page.locator("[data-inbox-evidence-doc='document:rulings']").click();
  const panel = page.locator("[data-inbox-doc-panel='document:rulings']");
  await expect(panel).toBeVisible();
  const box = await panel.boundingBox();
  expect(box.width).toBeGreaterThan(300);
  expect(box.x).toBeLessThan(40);
  // Focus moved into the sheet, so Escape reaches it and lands back on the
  // button that opened it rather than on <body>.
  await page.keyboard.press("Escape");
  await expect(panel).toHaveCount(0);
  await expect(
    page.locator("[data-inbox-evidence-doc='document:rulings']"),
  ).toBeFocused();
});
