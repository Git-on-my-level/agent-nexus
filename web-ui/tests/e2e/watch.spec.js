import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };
import { installQaRoutes } from "../../scripts/qa-visual.mjs";

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

test("empty visual QA scene clears the digest as well as initiatives", async ({
  page,
  context,
}) => {
  await setup(context);
  await installQaRoutes(page, {
    name: "workspace-watch-empty",
    workspaceMode: "workspace-default",
    briefState: "empty",
  });
  await page.goto(WATCH);
  await expect(page.locator("[data-watch-empty]")).toBeVisible();
  await expect(page.getByRole("region", { name: "Daily digest" })).toHaveCount(
    0,
  );
});

test("digest-only work stays visible in the Initiatives list", async ({
  page,
  context,
}) => {
  await setup(context, (route) =>
    json(route, {
      ...brief,
      initiatives: [],
      loops: [],
      generated_at: new Date().toISOString(),
    }),
  );
  await page.goto(WATCH);
  await expect(
    page.getByRole("heading", {
      name: `Needs an owner's look (${brief.daily_digest.length})`,
    }),
  ).toBeVisible();
  for (const card of brief.daily_digest) {
    const row = page.locator(`[data-collection-row="${card.ref}"]`);
    await expect(row).toBeVisible();
    await expect(row).toContainText(card.title);
    await expect(row.locator("a").first()).toHaveAttribute(
      "href",
      `${WATCH.replace("/watch", "")}/initiatives/${card.ref.slice(5)}`,
    );
    await expect(
      row.locator('[data-source="computed:daily_digest.reason"]').last(),
    ).toBeVisible();
  }
  await expect(page.locator("[data-watch-empty]")).toHaveCount(0);
});

for (const layout of ["list", "board"]) {
  test(`mixed ordinary and digest work stays visible in ${layout}`, async ({
    page,
    context,
  }) => {
    await setup(context);
    await page.goto(`${WATCH}?view=${layout}`);
    if (layout === "board") {
      await expect(
        page.locator(
          '[data-watch-column="review"] [data-work-ref="card:fleet-onboarding"]',
        ),
      ).toBeVisible();
    } else {
      await expect(
        page.locator('[data-collection-row="card:battery-measurement"]'),
      ).toBeVisible();
    }
    const digest = page.getByRole("region", { name: "Daily digest" });
    await expect(digest).toBeVisible();
    for (const card of brief.daily_digest) {
      await expect(
        digest.locator(`[data-collection-row="${card.ref}"]`),
      ).toContainText(card.title);
    }
  });
}

test("lists initiatives by computed state with sources tagged", async ({
  page,
  context,
}) => {
  await setup(context);
  await page.goto(WATCH);
  // Initiatives is where Watch lands; a decision for the reader sorts first.
  await expect(page.locator('[data-watch-tab="initiatives"]')).toHaveAttribute(
    "aria-selected",
    "true",
  );
  const rows = page.locator("[data-collection-row]");
  await expect(rows.first()).toHaveAttribute("data-state", "waiting_on_you");
  const battery = page.locator(
    '[data-collection-row="card:battery-measurement"]',
  );
  await expect(battery).toContainText("9d past");
  await expect(
    battery.locator('[data-source="computed:initiatives.past_promise_for"]'),
  ).toHaveText(/9d past/);
  await expect(
    page.locator('[data-source="computed:generated_at"]'),
  ).toContainText("as of");
  // Done and no-promise start folded.
  await expect(
    page.locator('[data-collection-row="card:docs-refresh"]'),
  ).toHaveCount(0);
  await page.locator("[data-collection-folded]").click();
  await expect(
    page.locator('[data-collection-row="card:docs-refresh"]'),
  ).toBeVisible();
  // Watch is read-only: the header points to Decide.
  await expect(page.locator("[data-watch-decide-link]")).toBeVisible();
  if (process.env.WATCH_SHOTS) {
    await page.setViewportSize({ width: 1400, height: 900 });
    await page.screenshot({
      path: `${process.env.WATCH_SHOTS}/watch-initiatives.png`,
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: `${process.env.WATCH_SHOTS}/watch-initiatives-phone.png`,
      fullPage: true,
    });
    await page.setViewportSize({ width: 1280, height: 720 });
  }
  // "You" in the next column links into Decide for that initiative.
  await battery
    .locator('a[data-source="computed:initiatives.waiting_on_you_count"]')
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
  await expect(page.locator("[data-collection-row]").first()).toBeVisible();
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
    // Series-only loops: no cadence, goal or receipts.
    loop: null,
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
  const loopsTab = page.locator('[data-watch-tab="loops"]');
  // The badge counts loops that need the reader: the two waiting on a decision.
  await expect(loopsTab).toContainText("2");
  await loopsTab.click();
  await expect(page).toHaveURL(/tab=loops/);
  const rows = page.locator("[data-collection-row]");
  await expect(rows.first()).toHaveAttribute("data-state", "waiting");
  const row = (ref) => page.locator(`[data-collection-row="${ref}"]`);
  await expect(row("card:loop-quiet")).toHaveAttribute("data-state", "silent");
  await expect(row("card:loop-flat")).toHaveAttribute("data-state", "flat");
  await expect(row("card:loop-moving")).toHaveAttribute("data-state", "moving");
  // Only loops: an initiative without a series is not listed.
  await expect(row("card:observability")).toHaveCount(0);
  await expect(row("card:loop-ask")).toContainText("0.42");
  // The state pills filter the rows.
  await page.locator('[data-collection-filter="flat"]').click();
  await expect(rows).toHaveCount(1);
});

/** Loops with run receipts: one per computed condition, one climbing a goal. */
function receiptLoopsBrief() {
  const now = Date.now();
  const at = (s) => new Date(now + s * 1000).toISOString();
  const DAY = 86400;
  const base = loopsBrief();
  const runs = (outcomes) =>
    outcomes.map((outcome, i) => ({
      at: at(-(outcomes.length - i) * DAY),
      outcome: outcome.split(":")[0],
      decision: outcome.split(":")[1] ?? null,
    }));
  const climb = Array.from({ length: 20 }, (_, i) => ({
    at: at(-(20 - i) * DAY),
    value: Number(
      (0.55 + 0.3 * (1 - Math.exp(-i / 6)) + (i % 3 === 1 ? -0.03 : 0)).toFixed(
        3,
      ),
    ),
    decision: i % 3 === 0 ? "kept" : i % 3 === 1 ? "reverted" : null,
  }));
  const loopRow = (id, title, loop) => ({
    ...base.initiatives.at(-1),
    ref: `card:${id}`,
    title,
    series: [],
    metric_flat: null,
    waiting_on_you_count: 0,
    loop,
  });
  // As core sends them: cards with receipts or a cadence live in `loops`,
  // never in `initiatives`.
  return {
    ...base,
    loops: [
      loopRow("avo-cleanup", "Disk cleanup optimizer", {
        health: "healthy",
        health_reason: "Kept a candidate 1d ago",
        every_seconds: DAY,
        progress_budget_seconds: 7 * DAY,
        last_run_at: at(-DAY),
        last_effect_at: at(-DAY),
        next_run_due_at: at(5 * 3600),
        recent_runs: runs([
          "acted:kept",
          "measured:reverted",
          "no_op",
          "acted:kept",
          "measured:rejected",
          "acted:kept",
        ]),
        runs_7d: { acted: 3, measured: 2, no_op: 1, infra_error: 0 },
        goal: {
          metric: "reclaimable_gb",
          direction: "up",
          target: 0.9,
          unit: "",
          latest: climb.at(-1).value,
          best: Math.max(...climb.map((p) => p.value)),
          baseline: 0.55,
          points: climb,
        },
      }),
      loopRow("cost-watch", "Cloud cost watchdog", {
        health: "failing",
        health_reason: "Last 2 runs hit infrastructure errors",
        every_seconds: DAY,
        last_run_at: at(-3600),
        last_effect_at: at(-6 * DAY),
        recent_runs: runs([
          "measured",
          "acted",
          "measured",
          "infra_error",
          "infra_error",
        ]),
        runs_7d: { acted: 1, measured: 2, no_op: 0, infra_error: 2 },
        goal: null,
      }),
      loopRow("pr-monitor", "PR completion monitor", {
        health: "stalled",
        health_reason: "No effect in 9d (budget 7d)",
        every_seconds: 6 * 3600,
        progress_budget_seconds: 7 * DAY,
        last_run_at: at(-2 * 3600),
        last_effect_at: at(-9 * DAY),
        recent_runs: runs([
          "no_op",
          "no_op",
          "measured",
          "no_op",
          "no_op",
          "no_op",
          "measured",
          "no_op",
        ]),
        runs_7d: { acted: 0, measured: 2, no_op: 6, infra_error: 0 },
        goal: null,
      }),
      loopRow("retention-check", "Retention check", {
        health: "overdue",
        health_reason: "No run in 3d (expected every 1d)",
        every_seconds: DAY,
        last_run_at: at(-3 * DAY),
        last_effect_at: at(-3 * DAY),
        next_run_due_at: at(-2 * DAY),
        recent_runs: runs(["measured", "acted"]),
        runs_7d: { acted: 1, measured: 1, no_op: 0, infra_error: 0 },
        goal: null,
      }),
    ],
  };
}

test("Loops with run receipts show cards, totals and the climb", async ({
  page,
  context,
}) => {
  await setup(context, (route) => json(route, receiptLoopsBrief()));
  await page.goto(`${WATCH}?view=loops`);
  const row = (ref) => page.locator(`[data-collection-row="${ref}"]`);
  await expect(row("card:cost-watch")).toHaveAttribute("data-state", "failing");
  await expect(row("card:retention-check")).toHaveAttribute(
    "data-state",
    "silent",
  );
  await expect(row("card:pr-monitor")).toHaveAttribute("data-state", "flat");
  // A loop's row shows its goal as a sparkline; expanding it shows the card.
  const climber = row("card:avo-cleanup");
  await expect(climber.locator("polyline")).toHaveCount(1);
  await climber.getByRole("button", { name: /Show details/ }).click();
  const card = climber.locator('[data-loop="card:avo-cleanup"]');
  await expect(card.locator("[data-loop-chart] polyline")).toHaveCount(2);
  await expect(card.locator("[data-loop-runs] li")).toHaveCount(6);
  await expect(card).toContainText("reclaimable_gb");
  await row("card:retention-check")
    .getByRole("button", { name: /Show details/ })
    .click();
  await expect(row("card:retention-check")).toContainText("run overdue by 2d");
  await expect(page.locator("[data-collection]")).not.toContainText("NaN");
  if (process.env.LOOP_SHOTS) {
    await page.setViewportSize({ width: 1400, height: 1500 });
    await page.screenshot({
      path: `${process.env.LOOP_SHOTS}/watch-loops.png`,
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 1600 });
    await page.screenshot({
      path: `${process.env.LOOP_SHOTS}/watch-loops-mobile.png`,
      fullPage: true,
    });
  }
});

test("loops core sends in brief.loops are on Loops, not Initiatives", async ({
  page,
  context,
}) => {
  const DAY = 86400;
  const at = (s) => new Date(Date.now() + s * 1000).toISOString();
  const loop = {
    ...brief.initiatives[0],
    ref: "card:nightly-cleanup",
    title: "Nightly cleanup loop",
    state: "moving",
    waiting_on_you_count: 0,
    series: [],
    metric_flat: null,
    loop: {
      health: "failing",
      health_reason: "Last 2 runs hit infrastructure errors",
      every_seconds: DAY,
      last_run_at: at(-3600),
      last_effect_at: at(-6 * DAY),
      recent_runs: [
        { at: at(-2 * DAY), outcome: "infra_error", decision: null },
        { at: at(-DAY), outcome: "infra_error", decision: null },
      ],
      runs_7d: { acted: 0, measured: 0, no_op: 0, infra_error: 2 },
      goal: null,
    },
  };
  await setup(context, (route) =>
    json(route, {
      ...brief,
      generated_at: new Date().toISOString(),
      // No measured initiatives: the only loop is the one in `loops`.
      initiatives: brief.initiatives.map((row) => ({
        ...row,
        series: [],
        metric_flat: null,
        loop: null,
      })),
      loops: [loop],
    }),
  );
  await page.goto(WATCH);
  await expect(page.locator("[data-collection-row]").first()).toBeVisible();
  await expect(
    page.locator('[data-collection-row="card:nightly-cleanup"]'),
  ).toHaveCount(0);
  await page.locator('[data-watch-tab="loops"]').click();
  await expect(page.locator("[data-watch-loops-empty]")).toHaveCount(0);
  await expect(
    page.locator('[data-collection-row="card:nightly-cleanup"]'),
  ).toHaveAttribute("data-state", "failing");
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
        loop: null,
      })),
      loops: [],
    }),
  );
  await page.goto(`${WATCH}?view=loops`);
  await expect(page.locator("[data-watch-loops-empty]")).toContainText(
    "anx work receipt",
  );
});

test("a failed dashboard read offers Retry instead of loading forever", async ({
  page,
  context,
}) => {
  test.setTimeout(120_000);
  const { visualReportExample } =
    await import("../../src/lib/fixtures/visualReportExample.js");
  await setup(context);
  let healthy = false;
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/reports",
    (route) =>
      healthy
        ? json(route, {
            status: "ok",
            pinned_ref: "document:alpha",
            reports: [
              {
                id: "alpha",
                ref: "document:alpha",
                segment: "alpha",
                title: "Dashboard alpha",
                report: visualReportExample,
              },
            ],
            has_more: false,
          })
        : json(route, { error: { message: "Private dashboard detail" } }, 500),
  );
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(page.getByText("Couldn't load dashboards.")).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.getByText("Private dashboard detail")).toHaveCount(0);
  healthy = true;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.locator("[data-watch-dashboard]")).toBeVisible();
  await expect(page.getByText("Couldn't load dashboards.")).toHaveCount(0);
});
