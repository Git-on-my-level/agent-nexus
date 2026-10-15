import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };

/**
 * Decide renders `brief.v1`. S9: one tap answers from a cold open on a phone.
 * Drafts share one durable key between the list and the permalink. A failed
 * read shows a banner with Retry, never the raw body (I5).
 */
const ACTOR = "actor-decide-e2e";
const LIST = "/o/local/w/local/decide";
const MERGE = brief.decisions.find((d) => d.inbox_item_id === "ask-91");

const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function setup(context, { respond, briefRoute } = {}) {
  await context.addInitScript((actor) => {
    localStorage.setItem("anx_ui_actor_id:local", actor);
    localStorage.setItem("workspaceTourSeen.local", "1");
  }, ACTOR);
  await context.route(/\/actors(?:\?.*)?$/, (route) =>
    json(route, {
      actors: [{ id: ACTOR, display_name: "Reader", tags: ["human"] }],
    }),
  );
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
    (url) => url.pathname === "/brief",
    briefRoute ??
      ((route) =>
        json(route, { ...brief, generated_at: new Date().toISOString() })),
  );
  const bodies = [];
  await context.route(/\/inbox\/[^/]+\/respond$/, (route) => {
    bodies.push(route.request().postDataJSON());
    return respond
      ? respond(route)
      : json(route, { event: { id: "response-one" } }, 201);
  });
  return bodies;
}

async function flush(page) {
  await page.evaluate(async () => {
    const queue = await import("/src/lib/inboxResponseQueue.js");
    await queue.flushInboxResponse();
  });
}

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("one tap answers from a cold open (S9)", async ({ page, context }) => {
    const bodies = await setup(context);
    await page.goto(LIST);
    const row = page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`);
    await expect(row).toBeVisible();
    // The order is decide_by first.
    await expect(page.locator("[data-decide-row]").first()).toHaveAttribute(
      "data-decide-row",
      "ask-88",
    );
    // The list is its own screen on a phone: tap the decision, then answer.
    await row.tap();
    const pane = page.locator(
      `[data-variant="pane"][data-decision-ref="${MERGE.ref}"]`,
    );
    await pane.locator('[data-decision-proposal="1"]').tap();
    // Answered: it leaves the list, the way an inbox clears.
    await expect(pane).toHaveCount(0);
    await flush(page);
    await expect.poll(() => bodies.length).toBe(1);
    expect(bodies[0]).toMatchObject({
      response_text: "Merge",
      outcome: "answered",
    });
    expect(bodies[0].idempotency_key).toBeTruthy();
  });
});

test("Undo takes an answer back before it is sent", async ({
  page,
  context,
}) => {
  const bodies = await setup(context);
  await page.goto(LIST);
  await page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`).click();
  const pane = page.locator(
    `[data-variant="pane"][data-decision-ref="${MERGE.ref}"]`,
  );
  await pane.locator('[data-decision-proposal="2"]').click();
  await expect(
    page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`),
  ).toHaveCount(0);
  await page.getByRole("button", { name: /undo/i }).click();
  // Undo puts the row back, selected, with its options.
  await expect(pane.locator('[data-decision-proposal="2"]')).toBeVisible();
  await page.waitForTimeout(500);
  expect(bodies).toHaveLength(0);
});

test("a reply draft survives moving between the list and the permalink", async ({
  page,
  context,
}) => {
  await setup(context);
  const TEXT = "Merge after the rig 4 rerun.";
  await page.goto(LIST);
  await page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`).click();
  const pane = page.locator(
    `[data-variant="pane"][data-decision-ref="${MERGE.ref}"]`,
  );
  await pane.locator("[data-decision-reply]").fill(TEXT);
  await page.getByRole("link", { name: "Open on its own page" }).click();
  await expect(page).toHaveURL(/\/decide\/ask-91$/);
  await expect(page.locator("[data-decision-reply]")).toHaveValue(TEXT);
  await page.goBack();
  await page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`).click();
  await expect(pane.locator("[data-decision-reply]")).toHaveValue(TEXT);
});

test("c copies the selected decision's ref; j/k move", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await setup(context);
  await page.goto(LIST);
  await expect(page.locator("[data-decide-row]").first()).toBeVisible();
  await page.keyboard.press("c");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    "inbox:ask-88",
  );
  await page.keyboard.press("j");
  await page.keyboard.press("c");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    MERGE.ref,
  );
  await page.keyboard.press("k");
  await expect(
    page.locator('[data-decide-row="ask-88"][aria-current="true"]'),
  ).toBeVisible();
});

test("number keys arm, then send; answering moves to the next", async ({
  page,
  context,
}) => {
  const bodies = await setup(context);
  await page.goto(LIST);
  await page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`).click();
  const pane = page.locator(
    `[data-variant="pane"][data-decision-ref="${MERGE.ref}"]`,
  );
  await page.keyboard.press("2");
  await expect(
    pane.locator('[data-decision-proposal="2"][data-decision-armed="true"]'),
  ).toBeVisible();
  await page.keyboard.press("2");
  await expect(
    page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`),
  ).toHaveCount(0);
  await expect(page.locator('[data-variant="pane"]')).toHaveCount(1);
  await flush(page);
  await expect.poll(() => bodies.length).toBe(1);
  expect(bodies[0]).toMatchObject({ response_text: "Hold a week" });
});

test("a failed read shows a banner with Retry, never the raw body (I5)", async ({
  page,
  context,
}) => {
  let calls = 0;
  await setup(context, {
    briefRoute: (route) => {
      calls += 1;
      return calls === 1
        ? json(route, { upstream: "cloudflare-secret-error-body" }, 524)
        : json(route, brief);
    },
  });
  await page.goto(LIST);
  await expect(page.getByText("Decisions didn’t load")).toBeVisible({
    timeout: 20_000,
  });
  await expect(page.getByText("cloudflare-secret-error-body")).toHaveCount(0);
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(page.locator("[data-decision-ref]").first()).toBeVisible();
});

test("old Inbox links land on Decide", async ({ page, context }) => {
  await setup(context);
  await page.goto("/o/local/w/local/inbox/ask-91");
  await expect(page).toHaveURL(/\/decide\/ask-91$/);
  await expect(page.locator("[data-decision-ref]")).toHaveCount(1);
});
