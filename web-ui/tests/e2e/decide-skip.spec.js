import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };

/**
 * Decide's multi-select and Skip all: clear a backlog without deciding it.
 * Every skipped ask is closed with outcome `skipped` and a note saying
 * nothing was decided; access requests and PM decisions are left for an
 * answer. Skips wait behind Undo, then go out through the durable outbox as
 * chunks of POST /inbox/responses, or one respond per item where the route is
 * not available.
 */
const ACTOR = "actor-decide-skip-e2e";
const LIST = "/o/local/w/local/decide";
const SKIPPABLE = ["ask-91", "ask-93", "ask-88"];

const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function setup(context, { batchRoute = true } = {}) {
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
    (route) =>
      json(route, { ...brief, generated_at: new Date().toISOString() }),
  );
  const sent = [];
  const batches = [];
  await context.route(/\/inbox\/responses$/, (route) => {
    if (!batchRoute)
      return json(
        route,
        { error: { code: "route_not_available", message: "not available" } },
        404,
      );
    const { items } = route.request().postDataJSON();
    batches.push(items.length);
    for (const { inbox_item_id: id, ...body } of items) sent.push({ id, body });
    return json(route, {
      results: items.map((_, index) => ({ index, status: "responded" })),
    });
  });
  sent.batches = batches;
  await context.route(/\/inbox\/[^/]+\/respond$/, (route) => {
    const id = decodeURIComponent(
      new URL(route.request().url()).pathname.split("/").at(-2),
    );
    sent.push({ id, body: route.request().postDataJSON() });
    return json(route, { event: { id: `response-${id}` } }, 201);
  });
  return sent;
}

test.describe("wide", () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  test("Skip all clears every skippable decision and tells each requester", async ({
    page,
    context,
  }, testInfo) => {
    const sent = await setup(context);
    await page.goto(LIST);
    await expect(page.locator("[data-decide-row]")).toHaveCount(5);
    // Access requests and PM decisions get no checkbox.
    await expect(page.locator("[data-decide-pick]")).toHaveCount(3);
    const bar = page.locator("[data-decide-bulk]");
    await expect(bar).toContainText("2 need an answer here");
    await testInfo.attach("decide-skip-bar", {
      body: await page.screenshot(),
      contentType: "image/png",
    });

    await bar.locator("[data-decide-skip-all]").click();
    await expect(page.locator("[data-decide-row]")).toHaveCount(2);
    await expect(page.locator("[data-decide-skip-toast]")).toContainText(
      "Skipped 3 decisions",
    );
    expect(sent).toHaveLength(0);

    await expect.poll(() => sent.length, { timeout: 15_000 }).toBe(3);
    // One request for the whole selection.
    expect(sent.batches).toEqual([3]);
    expect(sent.map((s) => s.id).sort()).toEqual([...SKIPPABLE].sort());
    for (const { body } of sent) {
      expect(body.outcome).toBe("skipped");
      expect(body.response_text).toMatch(/without a decision/i);
      expect(body.idempotency_key).toBeTruthy();
    }
    await expect(page.locator("[data-decide-row]")).toHaveCount(2);
    await expect(page.locator("[data-decide-skip-toast]")).toHaveCount(0);
  });

  test("falls back to one respond per decision where the batch route is missing", async ({
    page,
    context,
  }) => {
    const sent = await setup(context, { batchRoute: false });
    await page.goto(LIST);
    await page.locator("[data-decide-bulk] [data-decide-skip-all]").click();
    await expect.poll(() => sent.length, { timeout: 15_000 }).toBe(3);
    expect(sent.batches).toEqual([]);
    expect(sent.map((s) => s.id).sort()).toEqual([...SKIPPABLE].sort());
    await expect(page.locator("[data-decide-row]")).toHaveCount(2);
  });

  test("select some with clicks and X, then Undo puts them back", async ({
    page,
    context,
  }) => {
    const sent = await setup(context);
    await page.goto(LIST);
    await page.locator('[data-decide-pick="ask-91"]').check();
    // X ticks the highlighted row.
    await page.locator('[data-decide-row="ask-88"]').click();
    await page.keyboard.press("x");
    const bar = page.locator("[data-decide-bulk]");
    await expect(bar).toContainText("2 selected");
    await bar.locator("[data-decide-skip-selected]").click();
    await expect(page.locator("[data-decide-row]")).toHaveCount(3);
    await expect(page.locator('[data-decide-row="ask-93"]')).toBeVisible();

    await page.locator("[data-decide-skip-undo]").click();
    await expect(page.locator("[data-decide-row]")).toHaveCount(5);
    await page.waitForTimeout(6_000);
    expect(sent).toHaveLength(0);
  });

  test("select all toggles only the skippable rows", async ({
    page,
    context,
  }) => {
    await setup(context);
    await page.goto(LIST);
    await page.locator("[data-decide-select-all]").check();
    await expect(page.locator("[data-decide-bulk]")).toContainText(
      "3 selected",
    );
    await page.locator("[data-decide-select-all]").uncheck();
    await expect(page.locator("[data-decide-skip-all]")).toBeVisible();
  });
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("a card can be selected and skipped from the stack", async ({
    page,
    context,
  }, testInfo) => {
    const sent = await setup(context);
    await page.goto(LIST);
    await expect(page.locator("[data-decide-stack]")).toBeVisible();
    await page.locator('[data-decide-pick="ask-93"]').check();
    await page.locator("[data-decide-skip-selected]").click();
    await expect(page.locator('[data-decide-card="ask-93"]')).toHaveCount(0);
    await testInfo.attach("decide-skip-phone", {
      body: await page.screenshot(),
      contentType: "image/png",
    });
    await expect.poll(() => sent.length, { timeout: 15_000 }).toBe(1);
    expect(sent[0]).toMatchObject({
      id: "ask-93",
      body: { outcome: "skipped" },
    });
  });
});
