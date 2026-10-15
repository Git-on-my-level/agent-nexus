import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };

/**
 * Watch renders `brief.v1`: four computed states, a by-stage view, exact
 * counts, and a banner (never a raw body) when the read fails.
 */
const ACTOR = "actor-watch-e2e";
const WATCH = "/o/local/w/local/watch";

const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function setup(context, briefRoute) {
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
}

test("lists initiatives by computed state with sources tagged", async ({
  page,
  context,
}) => {
  await setup(context);
  await page.goto(WATCH);
  const groups = page.locator("[data-watch-group]");
  await expect(groups.first()).toHaveAttribute(
    "data-watch-group",
    "past_promise",
  );
  const battery = page.locator('[data-watch-row="card:battery-measurement"]');
  await expect(battery).toContainText("9d past");
  await expect(
    battery.locator('[data-source="computed:initiatives.past_promise_for"]'),
  ).toHaveText(/9d past/);
  await expect(
    page.locator('[data-source="computed:generated_at"]'),
  ).toContainText("as of");
  // Waiting-on-you is a badge linking into Decide, not a state.
  await battery
    .locator('[data-source="computed:initiatives.waiting_on_you_count"]')
    .click();
  await expect(page).toHaveURL(
    /\/decide\?initiative=card%3Abattery-measurement|\/decide\?initiative=card:battery-measurement/,
  );
});

test("the by-stage view shows the five placements", async ({
  page,
  context,
}) => {
  await setup(context);
  await page.goto(`${WATCH}?view=board`);
  for (const placement of ["backlog", "ready", "in_progress", "review", "done"])
    await expect(
      page.locator(`[data-watch-column="${placement}"]`),
    ).toBeVisible();
  await expect(
    page.locator(
      '[data-watch-column="review"] [data-work-ref="card:fleet-onboarding"]',
    ),
  ).toBeVisible();
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("by stage shows one column at a time with a counted switcher", async ({
    page,
    context,
  }) => {
    await setup(context);
    await page.goto(`${WATCH}?view=board`);
    const switcher = page.locator("[data-watch-stage-switcher]");
    await expect(switcher).toBeVisible();
    // Counts on every stage; the first stage with cards is on screen.
    await expect(
      switcher.locator('[data-watch-stage-tab="review"]'),
    ).toContainText("1");
    await expect(page.locator("[data-watch-column]:visible")).toHaveCount(1);
    await expect(page.locator('[data-watch-column="backlog"]')).toBeVisible();

    await switcher.locator('[data-watch-stage-tab="review"]').tap();
    await expect(
      switcher.locator('[data-watch-stage-tab="review"]'),
    ).toHaveAttribute("aria-pressed", "true");
    const review = page.locator('[data-watch-column="review"]');
    await expect(review).toBeVisible();
    await expect(page.locator("[data-watch-column]:visible")).toHaveCount(1);
    // Every card keeps its Move to menu, and nothing scrolls sideways.
    await expect(
      review.locator('[data-work-ref="card:fleet-onboarding"] select'),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
  });
});

test.describe("phone move", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("a card moved with Move to takes the switcher with it", async ({
    page,
    context,
  }) => {
    await setup(context);
    const now = new Date().toISOString();
    await context.route(/\/work\/card(?:%3A|:)labels/, (route) =>
      json(route, {
        work: {
          ref: "card:labels",
          id: "labels",
          title: "Label scheme",
          phase: "backlog",
          board_ref: "board:b1",
          version: 1,
          source: { authority: "nexus" },
          updated_at: now,
        },
      }),
    );
    await context.route(/\/boards\/(?:board(?:%3A|:))?b1/, (route) =>
      json(route, { board: { id: "b1", updated_at: now } }),
    );
    await context.route(/\/cards\/[^/]+\/move/, (route) =>
      json(route, { card: { id: "labels" } }),
    );
    await page.goto(`${WATCH}?view=board`);
    const card = page.locator(
      '[data-watch-column="backlog"] [data-work-ref="card:labels"]',
    );
    await expect(card).toBeVisible();
    await card.locator("select").selectOption("review");

    // The reader goes with it: Review is on screen, with the card focused.
    await expect(
      page.locator('[data-watch-stage-tab="review"]'),
    ).toHaveAttribute("aria-pressed", "true");
    const moved = page.locator(
      '[data-watch-column="review"] [data-work-ref="card:labels"]',
    );
    await expect(moved).toBeVisible();
    await expect(moved).toBeFocused();
    await expect(page.locator("[data-watch-column]:visible")).toHaveCount(1);
  });
});

test("a failed read shows a banner with Retry, never the raw body", async ({
  page,
  context,
}) => {
  let calls = 0;
  await setup(context, (route) => {
    calls += 1;
    return calls === 1
      ? json(route, { upstream: "cloudflare-secret-error-body" }, 524)
      : json(route, brief);
  });
  await page.goto(WATCH);
  await expect(page.getByRole("button", { name: "Retry" })).toBeVisible({
    timeout: 20_000,
  });
  await expect(page.getByText("cloudflare-secret-error-body")).toHaveCount(0);
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(page.locator("[data-watch-row]").first()).toBeVisible();
});
