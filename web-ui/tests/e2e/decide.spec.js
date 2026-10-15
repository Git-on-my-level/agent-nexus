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

  const card = (page, id) => page.locator(`[data-decide-card="${id}"]`);

  test("one tap answers from a cold open (S9)", async ({ page, context }) => {
    const bodies = await setup(context);
    await page.goto(LIST);
    // A phone gets a stack of whole decisions, not a list to tap into.
    await expect(page.locator("[data-decide-stack]")).toBeVisible();
    await expect(page.locator("[data-decide-row]")).toHaveCount(0);
    // The order is decide_by first.
    await expect(page.locator("[data-decide-card]").first()).toHaveAttribute(
      "data-decide-card",
      "ask-88",
    );
    const merge = card(page, MERGE.inbox_item_id);
    await expect(merge.locator("[data-decision-body]")).toContainText(
      "Calibration drift",
    );
    await merge.locator('[data-decision-proposal="1"]').tap();
    // Answered: the card folds to one line while the answer waits.
    await expect(merge.locator("[data-decide-answered]")).toBeVisible();
    await expect(merge.locator("[data-decision-proposal]")).toHaveCount(0);
    await flush(page);
    await expect.poll(() => bodies.length).toBe(1);
    expect(bodies[0]).toMatchObject({
      response_text: "Merge",
      outcome: "answered",
    });
    expect(bodies[0].idempotency_key).toBeTruthy();
    // Sent: it leaves the stack.
    await expect(merge).toHaveCount(0);
  });

  test("Undo on the folded card puts the decision back unsent", async ({
    page,
    context,
  }) => {
    const bodies = await setup(context);
    await page.goto(LIST);
    const merge = card(page, MERGE.inbox_item_id);
    await merge.locator('[data-decision-proposal="2"]').tap();
    await merge.locator("[data-decide-undo]").tap();
    await expect(merge.locator('[data-decision-proposal="2"]')).toBeVisible();
    await page.waitForTimeout(500);
    expect(bodies).toHaveLength(0);
  });

  test("?item= scrolls to its card and highlights it", async ({
    page,
    context,
  }) => {
    await setup(context);
    await page.goto(`${LIST}?item=ask-93`);
    const target = card(page, "ask-93");
    await expect(target).toHaveAttribute("data-highlighted", "true");
    await expect(target).toBeInViewport();
  });

  test("Reply keeps Send on screen and sends the text", async ({
    page,
    context,
  }) => {
    const bodies = await setup(context);
    await page.goto(LIST);
    const merge = card(page, MERGE.inbox_item_id);
    await merge.locator("[data-decision-reply-toggle]").tap();
    const box = merge.locator("[data-decision-reply]");
    await expect(box).toBeFocused();
    await box.fill("Merge after the rig 4 rerun.");
    const send = merge.getByRole("button", { name: "Send reply" });
    await expect(send).toBeInViewport();
    // Clear of the bottom nav.
    const nav = await page.locator(".shell-bottom-nav").boundingBox();
    const sendBox = await send.boundingBox();
    expect(sendBox.y + sendBox.height).toBeLessThanOrEqual(nav.y);
    await send.tap();
    await flush(page);
    await expect.poll(() => bodies.length).toBe(1);
    expect(bodies[0].response_text).toBe("Merge after the rig 4 rerun.");
  });
});

test.describe("phone composer", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  const LONG = {
    ...brief,
    decisions: brief.decisions.map((decision) =>
      decision.inbox_item_id === MERGE.inbox_item_id
        ? {
            ...decision,
            body: Array(24)
              .fill("Calibration drift is under 2% on five of six rigs.")
              .join("\n\n"),
          }
        : decision,
    ),
  };

  /** True when the element's centre is the element (nothing on top of it). */
  const uncovered = (locator) =>
    locator.evaluate((node) => {
      const box = node.getBoundingClientRect();
      const hit = document.elementFromPoint(
        box.x + box.width / 2,
        box.y + box.height / 2,
      );
      return Boolean(hit) && (hit === node || node.contains(hit));
    });

  test("a typed reply pins Send above the nav, then above the keyboard", async ({
    page,
    context,
  }) => {
    const bodies = await setup(context, {
      briefRoute: (route) =>
        json(route, { ...LONG, generated_at: new Date().toISOString() }),
    });
    await page.goto(LIST);
    const merge = page.locator(`[data-decide-card="${MERGE.inbox_item_id}"]`);
    await merge.locator("[data-decision-more]").tap();
    await merge.locator("[data-decision-reply-toggle]").tap();
    const field = merge.locator("[data-decision-reply]");
    await expect(field).toBeFocused();
    // The long body puts the composer far below the fold in flow.
    await merge.evaluate((node) => node.scrollIntoView({ block: "start" }));
    await field.fill("Merge after the rig 4 rerun.");

    const bar = merge.locator("[data-decision-composer]");
    await expect(bar).toHaveAttribute("data-pinned", "true");
    const send = merge.getByRole("button", { name: "Send reply" });
    expect(await uncovered(send)).toBe(true);
    expect(await uncovered(field)).toBe(true);
    const nav = await page.locator(".shell-bottom-nav").boundingBox();
    const sendBox = await send.boundingBox();
    expect(sendBox.y + sendBox.height).toBeLessThanOrEqual(nav.y);

    // An on-screen keyboard: the visual viewport loses its bottom 340px.
    const KEYBOARD = 340;
    await page.evaluate((keyboard) => {
      const viewport = window.visualViewport;
      Object.defineProperty(viewport, "height", {
        configurable: true,
        get: () => window.innerHeight - keyboard,
      });
      viewport.dispatchEvent(new Event("resize"));
    }, KEYBOARD);
    const top = 844 - KEYBOARD;
    await expect
      .poll(async () => {
        const box = await send.boundingBox();
        return box.y + box.height;
      })
      .toBeLessThanOrEqual(top);
    const fieldBox = await field.boundingBox();
    expect(fieldBox.y).toBeGreaterThanOrEqual(0);
    expect(fieldBox.y + fieldBox.height).toBeLessThanOrEqual(top);
    expect(await uncovered(send)).toBe(true);
    expect(await uncovered(field)).toBe(true);

    // Enter is a newline on a phone keyboard; Send is the way out.
    await field.press("Enter");
    await expect(field).toHaveValue("Merge after the rig 4 rerun.\n");
    await expect(field).toBeFocused();
    await send.tap();
    await flush(page);
    await expect.poll(() => bodies.length).toBe(1);
    expect(bodies[0].response_text).toBe("Merge after the rig 4 rerun.");
  });

  test("the permalink's reply pins while it is in use", async ({
    page,
    context,
  }) => {
    await setup(context, {
      briefRoute: (route) =>
        json(route, { ...LONG, generated_at: new Date().toISOString() }),
    });
    await page.goto(`${LIST}/${MERGE.inbox_item_id}`);
    const field = page.locator("[data-decision-reply]");
    await expect(page.locator("[data-decision-composer]")).not.toHaveAttribute(
      "data-pinned",
      "true",
    );
    await field.focus();
    await expect(page.locator("[data-decision-composer]")).toHaveAttribute(
      "data-pinned",
      "true",
    );
    await field.fill("Hold for rig 4.");
    expect(
      await uncovered(page.getByRole("button", { name: "Send reply" })),
    ).toBe(true);
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
  // Past the 5s undo window: a send that was only delayed would land now.
  await page.waitForTimeout(6_000);
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

test("a refused answer comes back with its recovery controls", async ({
  page,
  context,
}) => {
  await setup(context, {
    respond: (route) =>
      json(
        route,
        {
          error: {
            code: "notification_target_required",
            message: "The requester cannot be notified.",
          },
        },
        409,
      ),
  });
  await page.goto(LIST);
  await page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`).click();
  const pane = page.locator(
    `[data-variant="pane"][data-decision-ref="${MERGE.ref}"]`,
  );
  await pane.locator('[data-decision-proposal="1"]').click();
  await expect(
    page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`),
  ).toHaveCount(0);
  await flush(page);
  // The row returns, with a way to answer without notifying.
  await expect(
    page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`),
  ).toBeVisible();
  await page.locator(`[data-decide-row="${MERGE.inbox_item_id}"]`).click();
  await expect(pane.locator("[data-decision-answer-silently]")).toBeVisible();
});

test.describe("phone keyboard", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("number keys in the stack never answer a decision", async ({
    page,
    context,
  }) => {
    const bodies = await setup(context);
    await page.goto(LIST);
    await expect(page.locator("[data-decide-card]").first()).toBeVisible();
    await page.keyboard.press("1");
    await page.keyboard.press("1");
    await page.waitForTimeout(300);
    await expect(page.locator("[data-decide-card]")).toHaveCount(
      brief.decisions.length,
    );
    await expect(page.locator("[data-decide-answered]")).toHaveCount(0);
    await expect(page.locator("[data-decision-armed]")).toHaveCount(0);
    await flush(page);
    expect(bodies).toHaveLength(0);
  });

  test("a link to an item that is gone shows the stack", async ({
    page,
    context,
  }) => {
    await setup(context);
    await page.goto(`${LIST}?item=ask-gone`);
    await expect(page.locator("[data-decide-card]").first()).toBeVisible();
    await expect(page.locator("[data-highlighted]")).toHaveCount(0);
    await expect(page.locator('[data-variant="pane"]')).toHaveCount(0);
  });
});
