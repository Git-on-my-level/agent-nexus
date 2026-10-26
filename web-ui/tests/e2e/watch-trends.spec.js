import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };
import { visualReportExample } from "../../src/lib/fixtures/visualReportExample.js";

/**
 * Watch → Trends renders three `watch_trends.v1` groups: a card per panel
 * with its value, change, takeaway and sparkline, a 7/30/90-day toggle, and
 * a retry (never a spinner for ever) when one group fails.
 */
const ACTOR = "actor-watch-trends-e2e";
const WATCH = "/o/local/w/local/watch";

const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

/** A gently trending series of `n` points with a gap of unknown history. */
const series = (n, from, to, gap = 0) =>
  Array.from({ length: n }, (_, i) =>
    i < gap
      ? null
      : Math.max(
          0,
          Math.round(from + ((to - from) * i) / n + ((i * 7) % 5) - 2),
        ),
  );

function trends(group, days) {
  const n = days === 90 ? 13 : days;
  const base = {
    schema: "watch_trends.v1",
    group,
    generated_at: new Date().toISOString(),
    range_days: days,
    bucket: days === 90 ? "week" : "day",
    buckets: [],
  };
  const panel = (extra) => ({ truncated: false, rows: [], ...extra });
  const panels = {
    decisions: [
      panel({
        id: "asks_flow",
        title: "Asks raised vs answered",
        unit: "count",
        direction: "up_good",
        value: 41,
        previous: 52,
        raised: 58,
        raised_previous: 49,
        series: [
          { key: "opened", label: "Raised", points: series(n, 1, 3) },
          { key: "answered", label: "Answered", points: series(n, 2, 1) },
          {
            key: "withdrawn",
            label: "Withdrawn or retired",
            points: series(n, 0, 1),
          },
        ],
      }),
      panel({
        id: "answer_time",
        title: "Time to answer",
        unit: "seconds",
        direction: "down_good",
        value: 8280,
        previous: 3600,
        series: [
          {
            key: "median",
            label: "Median wait",
            points: series(n, 50, 140).map((v) => v * 60),
          },
        ],
      }),
      panel({
        id: "ask_backlog",
        title: "Open asks over time",
        unit: "count",
        direction: "down_good",
        value: 23,
        previous: 9,
        series: [{ key: "open", label: "Open asks", points: series(n, 9, 23) }],
      }),
      panel({
        id: "ask_sources",
        title: "Who asks for help most",
        unit: "count",
        direction: "down_good",
        value: null,
        previous: null,
        series: [],
        rows: [
          {
            principal: { id: "a1", display: "release-agent", kind: "agent" },
            value: 19,
            previous: 7,
          },
          {
            principal: { id: "a2", display: "docs-agent", kind: "agent" },
            value: 11,
            previous: 12,
          },
          {
            principal: { id: "a3", display: "triage-agent", kind: "agent" },
            value: 6,
            previous: 4,
          },
        ],
      }),
    ],
    initiatives: [
      panel({
        id: "promises",
        title: "Promises kept",
        unit: "percent",
        direction: "up_good",
        value: 62,
        previous: 78,
        due: 13,
        series: [
          { key: "kept", label: "Kept", points: series(n, 1, 0) },
          { key: "missed", label: "Missed", points: series(n, 0, 1) },
        ],
      }),
      panel({
        id: "past_promise",
        title: "Initiatives past their promise",
        unit: "percent",
        direction: "down_good",
        value: 75,
        previous: 50,
        past: 24,
        total: 32,
        series: [
          { key: "past", label: "Past promise", points: series(n, 50, 75) },
        ],
      }),
      panel({
        id: "work_flow",
        title: "Work started vs finished",
        unit: "count",
        direction: "up_good",
        value: 34,
        previous: 29,
        started: 47,
        started_previous: 31,
        series: [
          { key: "started", label: "Started", points: series(n, 1, 2) },
          { key: "finished", label: "Finished", points: series(n, 1, 1) },
        ],
      }),
    ],
    loops: [
      panel({
        id: "loop_runs",
        title: "Loop runs",
        unit: "count",
        direction: "up_good",
        value: null,
        previous: null,
        failed: null,
        truncated: true,
        series: [
          {
            key: "runs",
            label: "Runs",
            points: series(n, 4, 6, Math.floor(n / 3)),
          },
          {
            key: "failed",
            label: "Failed to run",
            points: series(n, 0, 1, Math.floor(n / 3)),
          },
        ],
      }),
      panel({
        id: "loop_health",
        title: "Loops reporting on time",
        unit: "percent",
        direction: "up_good",
        value: 82,
        previous: 95,
        series: [
          {
            key: "on_time",
            label: "Reporting on time",
            points: series(n, 95, 82),
          },
        ],
      }),
      panel({
        id: "agent_runs",
        title: "Most active agents",
        unit: "count",
        direction: "neutral",
        value: null,
        previous: null,
        series: [],
        rows: [
          {
            principal: { id: "a4", display: "eval-loop", kind: "agent" },
            value: 120,
            previous: null,
          },
          {
            principal: { id: "a1", display: "release-agent", kind: "agent" },
            value: 31,
            previous: null,
          },
        ],
      }),
    ],
  };
  return { ...base, panels: panels[group] };
}

async function setup(context, trendsRoute) {
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
  const seen = [];
  await context.route(
    (url) => url.pathname === "/watch/trends",
    (route) => {
      const url = new URL(route.request().url());
      const group = url.searchParams.get("group");
      const days = Number.parseInt(url.searchParams.get("range") || "30", 10);
      seen.push(`${group}:${days}`);
      return trendsRoute
        ? trendsRoute(route, group, days)
        : json(route, trends(group, days));
    },
  );
  return seen;
}

for (const [name, viewport] of [
  ["desktop", { width: 1280, height: 1100 }],
  ["phone", { width: 390, height: 844 }],
]) {
  test(`Trends shows change over time (${name})`, async ({ page, context }) => {
    await page.setViewportSize(viewport);
    const seen = await setup(context);
    await page.goto(WATCH);
    await page.locator('[data-watch-tab="trends"]').click();
    await expect(page).toHaveURL(/tab=trends/);
    const card = page.locator('[data-trend="answer_time"]');
    await expect(card).toBeVisible();
    await expect(card.locator("[data-trend-value]")).toHaveText("2.3 h");
    await expect(card.locator("[data-trend-takeaway]")).toHaveText(
      "Answer time up 2.3× vs the previous 30 days, now 2.3 h.",
    );
    await expect(
      page.locator('[data-trend="past_promise"] [data-trend-takeaway]'),
    ).toHaveText("24 of 32 initiatives are past their promise, up 25 pts.");
    await expect(
      page.locator('[data-trend="loop_runs"] [data-trend-partial]'),
    ).toBeVisible();
    expect(seen.sort()).toEqual(["decisions:30", "initiatives:30", "loops:30"]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
    if (process.env.WATCH_SHOTS)
      await page.screenshot({
        path: `${process.env.WATCH_SHOTS}/watch-trends-${name}.png`,
        fullPage: true,
      });
  });
}

test("the period toggle reads again for the chosen range", async ({
  page,
  context,
}) => {
  const seen = await setup(context);
  await page.goto(`${WATCH}?tab=trends`);
  await expect(page.locator('[data-trend="asks_flow"]')).toBeVisible();
  // The accessible name includes the visible label (WCAG 2.5.3).
  await expect(
    page.getByRole("button", { name: /^7d — last 7 days$/ }),
  ).toBeVisible();
  await page.locator('[data-watch-trend-days="7"]').click();
  await expect(page).toHaveURL(/range=7d/);
  await expect(page.locator('[data-watch-trend-days="7"]')).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect.poll(() => seen.filter((s) => s.endsWith(":7")).length).toBe(3);
  await expect(
    page.locator('[data-trend="answer_time"] [data-trend-takeaway]'),
  ).toContainText("previous week");
});

test("a failed group offers a retry and leaves the others", async ({
  page,
  context,
}) => {
  let healthy = false;
  await setup(context, (route, group, days) =>
    group === "loops" && !healthy
      ? json(route, { error: { message: "Private upstream detail" } }, 503)
      : json(route, trends(group, days)),
  );
  await page.goto(`${WATCH}?tab=trends`);
  await expect(page.locator('[data-trend="asks_flow"]')).toBeVisible();
  const loops = page.locator('[data-trend-group="loops"]');
  await expect(loops.locator("[data-trend-error]")).toBeVisible();
  await expect(page.getByText("Private upstream detail")).toHaveCount(0);
  healthy = true;
  await loops.getByRole("button", { name: "Retry" }).click();
  await expect(page.locator('[data-trend="loop_health"]')).toBeVisible();
});

test("the workspace default dashboard is not a standing tab", async ({
  page,
  context,
}) => {
  await setup(context);
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/pins",
    (route) =>
      json(route, {
        document_refs: ["document:alpha"],
        source: "workspace_default",
      }),
  );
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/reports",
    (route) =>
      json(route, {
        status: "ok",
        pinned_ref: "document:alpha",
        reports: [
          {
            id: "alpha",
            ref: "document:alpha",
            segment: "alpha",
            title: "Workspace dashboard",
            updated_at: "2026-10-13T00:00:00Z",
            revision_ref: "document_revision:alpha-1",
            report: visualReportExample,
          },
        ],
        has_more: false,
      }),
  );
  await page.goto(WATCH);
  await expect(page.locator("[data-watch-more-dashboards]")).toBeVisible();
  await expect(page.locator('[data-watch-tab="trends"]')).toBeVisible();
  await expect(page.locator('[data-watch-tab^="dashboard:"]')).toHaveCount(0);
  // Still one click away, and `?tab=dashboard` still opens it.
  await page.goto(`${WATCH}?tab=dashboard`);
  await expect(
    page.locator('[data-watch-tab="dashboard:alpha"]'),
  ).toHaveAttribute("aria-selected", "true");
});

test("restricted trends explain unavailability without retrying", async ({
  page,
  context,
}) => {
  await setup(context, (route) =>
    json(
      route,
      {
        error: {
          code: "restricted_trends",
          message: "Internal detail must not be shown",
        },
      },
      403,
    ),
  );
  await page.goto(`${WATCH}?tab=trends`);
  await expect(page.locator("[data-trend-restricted]")).toHaveCount(3);
  await expect(
    page.getByText("Trends are unavailable with your current access.", {
      exact: false,
    }),
  ).toHaveCount(3);
  await expect(
    page.getByRole("button", { name: "Retry", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText("Internal detail must not be shown")).toHaveCount(
    0,
  );
  await expect(page.locator("[aria-busy=true]")).toHaveCount(0);
});
