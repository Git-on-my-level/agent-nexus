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

/** The fixture plus loops in every Loops group. */
function loopsBrief() {
  const now = Date.now();
  const ago = (s) => new Date(now - s * 1000).toISOString();
  const base = brief.initiatives.find((row) => row.series?.length);
  const make = (id, title, extra) => ({
    ...base,
    ref: `card:${id}`,
    title,
    state: "moving",
    past_promise_for_seconds: 0,
    waiting_on_you_count: 0,
    last_evidence_at: ago(3600),
    possibly_moot: { flagged: false },
    note: null,
    metric_flat: null,
    series: [
      {
        ref: `series:${id}`,
        name: `${id} rate`,
        labels: {},
        last_value: 0.42,
        last_at: ago(1800),
      },
    ],
    ...extra,
  });
  return {
    ...brief,
    generated_at: new Date().toISOString(),
    initiatives: [
      ...brief.initiatives,
      make("loop-ask", "Signup experiment", { waiting_on_you_count: 1 }),
      make("loop-quiet", "Retention nudges", {
        last_evidence_at: ago(9 * 86400),
      }),
      make("loop-flat", "Pricing page test", {
        metric_flat: {
          flat: true,
          for_seconds: 4 * 86400,
          window_seconds: 604800,
        },
      }),
      make("loop-moving", "Onboarding emails", {
        metric_flat: { flat: false, for_seconds: 0, window_seconds: 604800 },
      }),
    ],
  };
}

test("Loops lists measured initiatives by what they need", async ({
  page,
  context,
}) => {
  await setup(context, (route) => json(route, loopsBrief()));
  await page.goto(WATCH);
  const loopsButton = page.locator('[data-watch-view="loops"]');
  // The fixture's loop plus four added ones.
  await expect(loopsButton).toContainText("5");
  await loopsButton.click();
  await expect(page).toHaveURL(/view=loops/);
  const groups = page.locator("[data-watch-loop-group]");
  await expect(groups.first()).toHaveAttribute(
    "data-watch-loop-group",
    "waiting",
  );
  await expect(page.locator('[data-watch-loop-group="silent"]')).toContainText(
    "Retention nudges",
  );
  await expect(page.locator('[data-watch-loop-group="flat"]')).toContainText(
    "Pricing page test",
  );
  await expect(
    page.locator(
      '[data-watch-loop-group="flat"] [data-source="computed:initiatives.metric_flat"]',
    ),
  ).toContainText(/^flat /);
  await expect(page.locator('[data-watch-loop-group="moving"]')).toContainText(
    "Onboarding emails",
  );
  // Only loops: an initiative without a series is not listed.
  await expect(
    page.locator('[data-watch-row="card:observability"]'),
  ).toHaveCount(0);
  await expect(
    page.locator('[data-watch-row="card:loop-ask"] [data-watch-metrics]'),
  ).toContainText("0.42");
});

test("Loops says how to add one when there are none", async ({
  page,
  context,
}) => {
  await setup(context, (route) =>
    json(route, {
      ...brief,
      generated_at: new Date().toISOString(),
      initiatives: brief.initiatives.map((row) => ({
        ...row,
        series: [],
        metric_flat: null,
      })),
    }),
  );
  await page.goto(`${WATCH}?view=loops`);
  await expect(page.locator("[data-watch-loops-empty]")).toContainText(
    "series:",
  );
});
