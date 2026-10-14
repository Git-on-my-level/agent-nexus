import { expect, test } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

/**
 * A filed ask batch (POST /asks/batch) renders as ONE inbox item with one
 * decision section per ask and a single submit. Core records every answer
 * atomically, so the UI submits exactly one request carrying all answers,
 * through the same durable inbox queue as a single reply.
 */

const ACTOR = "actor-batch-e2e";

const BATCH_ASKS = [
  {
    request_event_ref: "event:ask-batch-1",
    title: "Ship the flag today?",
    body: "The rollout is staged and verified.",
    response_proposals: ["Ship it.", "Hold until tomorrow."],
    delivery: "batch",
  },
  {
    request_event_ref: "event:ask-batch-2",
    title: "Roll back the canary first?",
    response_proposals: ["No, keep it.", "Roll back."],
    delivery: "batch",
  },
  {
    request_event_ref: "event:ask-batch-3",
    title: "Notify the field team?",
    response_proposals: ["Yes, post in the field room."],
    delivery: "none",
  },
];

const ITEM = {
  id: "inbox-batch-e2e",
  kind: "ask",
  status: "open",
  title: "Ship the flag today?",
  body: "",
  thread_id: "thread-batch",
  subject_ref: "thread:thread-batch",
  source_event_id: "ev-batch-1",
  request_event_ref: "event:ask-batch-1",
  related_refs: ["thread:thread-batch"],
  response_proposals: BATCH_ASKS[0].response_proposals,
  batch: { id: "askbatch-e2e", asks: BATCH_ASKS },
  notification_target_status: { resolvable: false },
  source_event_time: new Date().toISOString(),
};

const LIST_URL = "/o/local/w/local/inbox";
const ITEM_URL = `${LIST_URL}/${encodeURIComponent(ITEM.id)}`;

const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function setup(context, { answered = false, send }) {
  await context.addInitScript((actor) => {
    localStorage.setItem("anx_ui_actor_id:local", actor);
    localStorage.setItem("workspaceTourSeen.local", "1");
  }, ACTOR);
  await context.route(/\/actors(?:\?.*)?$/, (route) =>
    json(route, {
      actors: [{ id: ACTOR, display_name: "Reader", tags: ["human"] }],
    }),
  );
  for (const [pattern, body] of [
    [/\/pm\/decisions(?:\?.*)?$/, { items: [] }],
    [/\/pm\/actions(?:\?.*)?$/, { items: [] }],
    [/\/work(?:\?.*)?$/, { work: [] }],
    [/\/home\/unread(?:\?.*)?$/, { groups: [] }],
    [/\/events(?:\?.*)?$/, { events: [] }],
    [
      /\/asks\/[^/?]+(?:\?.*)?$/,
      { ask_id: "event:ask-batch-1", status: "open", delivery: [] },
    ],
  ])
    await context.route(pattern, (route) => json(route, body));
  await context.route(
    /\/(?:stream\/(?:events|inbox)|inbox\/stream)(?:\?.*)?$/,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "",
      }),
  );
  await context.route(
    (url) => url.pathname === "/inbox",
    (route) => json(route, { status: "open", items: answered ? [] : [ITEM] }),
  );
  await context.route(
    (url) => url.pathname === `/inbox/${ITEM.id}`,
    (route) => json(route, { item: ITEM }),
  );
  await context.route(
    (url) => url.pathname === `/inbox/${ITEM.id}/respond`,
    (route) => {
      send?.(route);
      if (!send) return json(route, { batch_id: "askbatch-e2e" }, 201);
      return undefined;
    },
  );
}

function sectionReply(page, ref) {
  return page.locator(`[data-inbox-batch-reply="${ref}"]`);
}

async function fillAll(page) {
  for (const [index, ask] of BATCH_ASKS.entries()) {
    await sectionReply(page, ask.request_event_ref).fill(`Answer ${index + 1}`);
  }
}

async function submitEnabled(page) {
  const submit = page.locator("[data-inbox-batch-submit]");
  await expect(submit).toBeEnabled();
  return submit;
}

test("a batch renders as one inbox item with one section per ask", async ({
  page,
  context,
}, testInfo) => {
  await setup(context, {});
  page.on("pageerror", (error) => console.log("PAGEERROR:", error.message));
  page.on("console", (message) => {
    if (message.type() === "error") console.log("CONSOLE:", message.text());
  });
  await page.goto(LIST_URL);
  // One row, titled from the batch's first ask — not three rows.
  await expect(page.getByTestId(`inbox-row-${ITEM.id}`)).toHaveCount(1);
  await page.getByTestId(`inbox-row-${ITEM.id}`).click();
  await expect(page.locator("[data-inbox-batch-panel]")).toBeVisible();
  await expect(page.locator("[data-inbox-batch-count]")).toContainText(
    "3 asks",
  );
  for (const ask of BATCH_ASKS) {
    await expect(
      page.locator(`[data-inbox-batch-section="${ask.request_event_ref}"]`),
    ).toBeVisible();
  }
  // No decision, no submit: the batch answers every member or nothing.
  await expect(page.locator("[data-inbox-batch-submit]")).toBeDisabled();
  await sectionReply(page, BATCH_ASKS[0].request_event_ref).fill("Ship it.");
  await expect(page.locator("[data-inbox-batch-submit]")).toBeDisabled();
  await fillAll(page);
  await expect(page.locator("[data-inbox-batch-submit]")).toBeEnabled();

  const dir = process.env.BATCH_SCREENSHOT_DIR;
  if (dir) {
    mkdirSync(dir, { recursive: true });
    await page.evaluate(() => window.scrollTo(0, 0));
    await page
      .screenshot({ path: join(dir, "batch-inbox-item.png"), fullPage: true })
      .catch(() => {});
  }
  void testInfo;
});

test("one submit records every answer in a single request", async ({
  page,
  context,
}, testInfo) => {
  const bodies = [];
  await setup(context, {
    send: (route) => {
      bodies.push(route.request().postDataJSON());
      return json(route, { batch_id: "askbatch-e2e" }, 201);
    },
  });
  await page.goto(ITEM_URL);
  await expect(page.locator("[data-inbox-batch-panel]")).toBeVisible();
  await fillAll(page);
  // One section goes back for context instead of being answered.
  await page
    .locator(`[data-inbox-batch-context="${BATCH_ASKS[2].request_event_ref}"]`)
    .click();
  await (await submitEnabled(page)).click();

  await expect
    .poll(() => bodies.length, { timeout: 10_000 })
    .toBeGreaterThanOrEqual(1);
  const request = bodies[0];
  expect(request.response_text).toBeUndefined();
  expect(request.outcome).toBeUndefined();
  expect(request.answers).toHaveLength(3);
  expect(request.answers.map((answer) => answer.ask_ref)).toEqual(
    BATCH_ASKS.map((ask) => ask.request_event_ref),
  );
  expect(request.answers[2].outcome).toBe("needs_context");
  expect(request.answers[0].outcome).toBe("answered");
  expect(typeof request.idempotency_key).toBe("string");

  // The batch leaves the open inbox once the one request confirms.
  await page.goto(LIST_URL);
  await expect(page.getByTestId(`inbox-row-${ITEM.id}`)).toHaveCount(0);

  const dir = process.env.BATCH_SCREENSHOT_DIR;
  if (dir) {
    mkdirSync(dir, { recursive: true });
    writeFileSync(
      join(dir, "batch-respond-request.json"),
      JSON.stringify(request, null, 2),
    );
  }
  void testInfo;
});

test("per-section drafts survive a reload", async ({ page, context }) => {
  await setup(context, {});
  await page.goto(ITEM_URL);
  await expect(page.locator("[data-inbox-batch-panel]")).toBeVisible();
  await sectionReply(page, BATCH_ASKS[0].request_event_ref).fill("Ship it.");
  await sectionReply(page, BATCH_ASKS[2].request_event_ref).fill(
    "Post in the field room.",
  );
  await page.reload();
  await expect(page.locator("[data-inbox-batch-panel]")).toBeVisible();
  await expect(sectionReply(page, BATCH_ASKS[0].request_event_ref)).toHaveValue(
    "Ship it.",
  );
  await expect(sectionReply(page, BATCH_ASKS[1].request_event_ref)).toHaveValue(
    "",
  );
  await expect(sectionReply(page, BATCH_ASKS[2].request_event_ref)).toHaveValue(
    "Post in the field room.",
  );
});

test("undo hands each section's draft back in place, without navigating", async ({
  page,
  context,
}) => {
  await setup(context, {});
  await page.goto(ITEM_URL);
  await expect(page.locator("[data-inbox-batch-panel]")).toBeVisible();
  for (const [index, ask] of BATCH_ASKS.entries()) {
    await sectionReply(page, ask.request_event_ref).fill(`Answer ${index + 1}`);
  }
  // One section goes back for context; Undo must reopen it in that state.
  await page
    .locator(`[data-inbox-batch-context="${BATCH_ASKS[2].request_event_ref}"]`)
    .click();
  await (await submitEnabled(page)).click();
  await expect(sectionReply(page, BATCH_ASKS[0].request_event_ref)).toHaveValue(
    "",
  );
  await page.getByRole("button", { name: "Undo" }).click();
  // The open composer refills in place: no reload, no second navigation.
  for (const [index, ask] of BATCH_ASKS.entries()) {
    await expect(sectionReply(page, ask.request_event_ref)).toHaveValue(
      `Answer ${index + 1}`,
    );
  }
  await expect(
    page.locator(
      `[data-inbox-batch-context="${BATCH_ASKS[2].request_event_ref}"]`,
    ),
  ).toHaveAttribute("aria-pressed", "true");
  // The restored batch is still submittable.
  await expect(page.locator("[data-inbox-batch-submit]")).toBeEnabled();
});
