import { expect, test } from "@playwright/test";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };
import { installQaRoutes } from "../../scripts/qa-visual.mjs";
import { visualReportExample } from "../../src/lib/fixtures/visualReportExample.js";

/**
 * Watch renders `brief.v1`: four computed states, rows that open inline,
 * the swimlane board as its Lanes view, a tab row that never draws a
 * scrollbar, exact counts, and a banner (never a raw body) when the read
 * fails.
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

test("the empty state still reads recent changes to the initiative set", async ({
  page,
  context,
}) => {
  // The set can be empty because its last initiative was just retired; who
  // retired it and why still reads beside the empty state.
  await setup(context, (route) =>
    json(route, {
      ...brief,
      initiatives: [],
      loops: [],
      daily_digest: [],
      generated_at: new Date().toISOString(),
      initiative_budget: { count: 0, target: 5, truncated: false },
      over_target: null,
      initiative_changes: {
        items: [
          {
            ref: "event:retired-last",
            ts: new Date(Date.now() - 3 * 3_600_000).toISOString(),
            type: "initiative_retired",
            actor_id: "agent:codex",
            payload: {
              subject_ref: "card:observability",
              title: "Observability",
              reason: "Outcome reached",
              before: { board_ref: "board:initiatives", initiative: true },
              after: { board_ref: "board:tasks", initiative: false },
            },
          },
        ],
        truncated: false,
      },
    }),
  );
  await page.goto(WATCH);
  await expect(page.locator("[data-watch-empty]")).toBeVisible();
  const changes = page.locator("[data-initiative-changes]");
  await expect(changes).toBeVisible();
  await expect(changes).toContainText("Retired");
  await expect(changes).toContainText("Observability");
  await expect(page.locator("[data-rollup-list]")).toHaveCount(0);
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

test("recent changes read beside digest-only work with no initiatives", async ({
  page,
  context,
}) => {
  // The rollup is empty but the digest is not, so neither the empty state
  // nor the rollup list draws; the changes feed must still read, once.
  await setup(context, (route) =>
    json(route, {
      ...brief,
      initiatives: [],
      loops: [],
      generated_at: new Date().toISOString(),
      initiative_budget: { count: 0, target: 5, truncated: false },
      over_target: null,
      initiative_changes: {
        items: [
          {
            ref: "event:retired-into-digest",
            ts: new Date(Date.now() - 3 * 3_600_000).toISOString(),
            type: "initiative_retired",
            actor_id: "agent:codex",
            payload: {
              subject_ref: "card:observability",
              title: "Observability",
              reason: "Outcome reached",
              before: { board_ref: "board:initiatives", initiative: true },
              after: { board_ref: "board:tasks", initiative: false },
            },
          },
        ],
        truncated: false,
      },
    }),
  );
  await page.goto(WATCH);
  await expect(
    page.getByRole("heading", {
      name: `Needs an owner's look (${brief.daily_digest.length})`,
    }),
  ).toBeVisible();
  await expect(page.locator("[data-watch-empty]")).toHaveCount(0);
  await expect(page.locator("[data-rollup-list]")).toHaveCount(0);
  const changes = page.locator("[data-initiative-changes]");
  await expect(changes).toBeVisible();
  await expect(changes).toHaveCount(1);
  await expect(changes).toContainText("Retired");
  await expect(changes).toContainText("Observability");
});

for (const layout of ["list", "lanes"]) {
  test(`mixed ordinary and digest work stays visible in ${layout}`, async ({
    page,
    context,
  }) => {
    await setup(context);
    await page.goto(`${WATCH}?view=${layout}`);
    if (layout === "lanes") {
      await expect(
        page.locator('[data-lane-ref="card:fleet-onboarding"]'),
      ).toBeVisible();
    } else {
      await expect(
        page.locator('[data-collection-row="card:fleet-onboarding"]'),
      ).toBeVisible();
    }
    const digest = page.getByRole("region", { name: "Daily digest" });
    await expect(digest).toBeVisible();
    await digest.scrollIntoViewIfNeeded();
    await expect(digest).toBeInViewport();
    for (const card of brief.daily_digest) {
      await expect(
        digest.locator(`[data-collection-row="${card.ref}"]`),
      ).toContainText(card.title);
    }
    if (process.env.WATCH_SHOTS) {
      await page.screenshot({
        path: `${process.env.WATCH_SHOTS}/digest-${layout}.png`,
      });
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
  await expect(rows.first()).toHaveAttribute("data-bucket", "needs_you");
  // A measured initiative is a loop: it is on Loops, not here.
  await expect(
    page.locator('[data-collection-row="card:battery-measurement"]'),
  ).toHaveCount(0);
  const fleet = page.locator('[data-collection-row="card:fleet-onboarding"]');
  await expect(fleet.locator("[data-rollup-next]")).toHaveText(
    "next: enroll m6 · Oct 16",
  );
  await expect(
    page.locator('[data-source="computed:generated_at"]'),
  ).toContainText("as of");
  // Done starts folded.
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
  await fleet.locator("[data-rollup-owner] a").click();
  await expect(page).toHaveURL(
    /\/decide\?initiative=card%3Afleet-onboarding|\/decide\?initiative=card:fleet-onboarding/,
  );
});

/** The brief, with children and an owner on Observability. */
function briefWithRollup() {
  const children = [
    ["card:enroll-m6", "Enroll m6", "in_progress"],
    ["card:runbook", "Runbook review", "review"],
    ["card:keys", "Rotate fleet keys", "done"],
  ].map(([ref, title, state]) => ({
    ref,
    title,
    step_id: ref.slice(5),
    state,
    past_promise: false,
    needs_viewer: false,
  }));
  return {
    ...brief,
    generated_at: new Date().toISOString(),
    initiatives: brief.initiatives.map((row) =>
      row.ref === "card:observability"
        ? {
            ...row,
            owner: "actor-luna",
            rollup: {
              total: 4,
              by_state: { in_progress: 1, review: 1, done: 1, backlog: 1 },
              past_promise: 0,
              needing_viewer: 0,
              workstreams: [],
              children,
              has_more: false,
            },
          }
        : row,
    ),
  };
}

/** One `work.get` for Observability; counts how often it is read. */
async function routeObservabilityWork(context) {
  const reads = { count: 0 };
  await context.route(/\/work\/card(?:%3A|:)observability(?:\?|$)/, (route) => {
    reads.count += 1;
    return json(route, {
      work: {
        ref: "card:observability",
        id: "observability",
        title: "Observability",
        phase: "in_progress",
        owner: "actor-luna",
        plan: {
          steps: [
            { id: "image", title: "Bake the m6 image" },
            {
              id: "enroll",
              title: "Enroll m6 in the fleet",
              after: ["image"],
            },
          ],
        },
        plan_state: { next_steps: ["image"], critical_path: ["image"] },
        summary: { status: { state: "moving", label: "Moving", reason: "" } },
      },
    });
  });
  return reads;
}

test.describe("lanes", () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  test("Lanes is the swimlane board, kept in the URL across reloads", async ({
    page,
    context,
  }) => {
    await setup(context, (route) => json(route, briefWithRollup()));
    await context.route(
      (url) => url.pathname === "/work/ungrouped",
      (route) =>
        json(route, {
          work: [
            {
              ref: "card:fleet-alerts",
              title: "Fleet alert routing",
              phase: "backlog",
              parent_ref: null,
              created_at: "2026-10-15T08:00:00Z",
              likely_home: [
                {
                  ref: "card:observability",
                  title: "Observability",
                  reasons: ["title_family"],
                },
              ],
              next_actions: [],
            },
          ],
          has_more: false,
        }),
    );
    await page.goto(WATCH);
    const toggle = page.locator("[data-watch-view-toggle]");
    await expect(toggle.locator("button")).toHaveText(["List", "Lanes"]);
    await toggle.locator('[data-watch-view="lanes"]').click();
    await expect(page).toHaveURL(/\/watch\?view=lanes$/);
    const board = page.locator("[data-swimlane-board]");
    await expect(
      board.locator(
        '[data-lane-ref="card:observability"] [data-work-ref="card:runbook"]',
      ),
    ).toBeVisible();
    // The tray and the budget chip come with it; Decide stays in the header.
    // The chip carries the board's target (the fixture's 5), not the legacy
    // default, so it reads over target and offers folds.
    await expect(board.locator("[data-swimlane-tray]")).toContainText(
      "Fleet alert routing",
    );
    await expect(board.locator("[data-initiative-budget]")).toHaveText(
      "Initiatives 6/5 · suggest folds",
    );
    await expect(page.locator("[data-watch-decide-link]")).toHaveCount(1);
    await expect(board.getByText(/in Decide →/)).toHaveCount(0);
    if (process.env.WATCH_SHOTS)
      await page.screenshot({
        path: `${process.env.WATCH_SHOTS}/watch-lanes.png`,
        fullPage: true,
      });

    await page.reload();
    await expect(
      page.locator('[data-lane-ref="card:observability"]'),
    ).toBeVisible();
    await expect(page.locator('[data-watch-view="lanes"]')).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    await page.locator('[data-watch-view="list"]').click();
    await expect(page).toHaveURL(/\/watch$/);
    await expect(
      page.getByRole("region", { name: "Initiatives", exact: true }),
    ).toBeVisible();
  });
});

test("a row opens inline on click and closes on the next", async ({
  page,
  context,
}) => {
  await setup(context, (route) => json(route, briefWithRollup()));
  const reads = await routeObservabilityWork(context);
  await page.goto(WATCH);
  // Rows that need the reader start open, without reading anything.
  const fleet = page.locator('[data-collection-row="card:fleet-onboarding"]');
  await expect(fleet).toHaveAttribute("data-expanded", "true");
  await expect(fleet.locator("[data-details-load]")).toBeVisible();

  const row = page.locator('[data-collection-row="card:observability"]');
  const title = row.locator("[data-collection-toggle]");
  await expect(title).toHaveAttribute("aria-expanded", "false");
  // Nothing is read until a row opens.
  expect(reads.count).toBe(0);

  // Anywhere on the row: here, the empty middle of it.
  await row
    .locator(".grid-row")
    .first()
    .click({ position: { x: 600, y: 12 } });
  const details = row.locator("[data-collection-details]");
  await expect(details).toBeVisible();
  await expect(title).toHaveAttribute("aria-expanded", "true");
  // Its children as kid rows, each with its own state.
  await expect(row.locator("[data-rollup-kid]")).toHaveCount(3);
  await expect(row.locator('[data-rollup-kid="card:runbook"]')).toContainText(
    "In review",
  );
  await expect(row.locator("[data-rollup-next]")).toContainText(
    "next: report progress",
  );
  await expect(details.locator("[data-details-evidence]")).toContainText(
    "Last evidence",
  );
  await expect(details.locator("[data-details-owner]")).toContainText("luna");
  await expect(details.locator("[data-details-steps]")).toContainText(
    "Bake the m6 image",
  );
  await expect(details.locator("[data-details-open]")).toHaveAttribute(
    "href",
    /\/initiatives\/observability$/,
  );
  if (process.env.WATCH_SHOTS)
    await page.screenshot({
      path: `${process.env.WATCH_SHOTS}/watch-row-expanded.png`,
      fullPage: true,
    });

  // Several rows can be open; each closes on its own.
  await expect(fleet).toHaveAttribute("data-expanded", "true");
  await row
    .locator(".grid-row")
    .first()
    .click({ position: { x: 600, y: 12 } });
  await expect(details).toHaveCount(0);
  await expect(title).toHaveAttribute("aria-expanded", "false");
  await expect(fleet).toHaveAttribute("data-expanded", "true");

  // Opening it again is answered from the cache: one read in all.
  await title.click();
  await expect(details.locator("[data-details-steps]")).toContainText(
    "Bake the m6 image",
  );
  expect(reads.count).toBe(1);
});

test("a row opens and closes from the keyboard", async ({ page, context }) => {
  await setup(context, (route) => json(route, briefWithRollup()));
  await routeObservabilityWork(context);
  await page.goto(WATCH);
  const row = page.locator('[data-collection-row="card:observability"]');
  const title = row.locator("[data-collection-toggle]");
  await title.focus();
  await page.keyboard.press("Enter");
  await expect(title).toHaveAttribute("aria-expanded", "true");
  await expect(row.locator("[data-collection-details]")).toBeVisible();
  await page.keyboard.press("Space");
  await expect(title).toHaveAttribute("aria-expanded", "false");
  await expect(row.locator("[data-collection-details]")).toHaveCount(0);
});

/** Pinned dashboards with long titles, so the tab row overflows. */
async function routeDashboards(context) {
  const ids = ["omi", "fleet", "margin", "retention", "billing"];
  const report = (id) => ({
    id,
    ref: `document:${id}`,
    segment: id,
    title: `${id[0].toUpperCase()}${id.slice(1)} weekly dashboard`,
    updated_at: "2026-10-13T00:00:00Z",
    revision_ref: `document_revision:${id}-1`,
    report: visualReportExample,
  });
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/reports",
    (route) =>
      json(route, {
        status: "ok",
        pinned_ref: "document:omi",
        reports: [...ids, "extra"].map(report),
        has_more: false,
      }),
  );
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/pins",
    (route) =>
      json(route, {
        document_refs: ids.map((id) => `document:${id}`),
        source: "personal",
      }),
  );
}

/** What the tab list's box says about overflow and scrollbars. */
const tabBox = (page) =>
  page.locator("[data-watch-tabs]").evaluate((el) => {
    const style = getComputedStyle(el);
    return {
      overflowY: style.overflowY,
      scrollbarWidth: style.scrollbarWidth,
      verticalOverflow: el.scrollHeight - el.clientHeight,
      // A drawn scrollbar takes room from the client box.
      scrollbarX: el.offsetHeight - el.clientHeight,
      scrollbarY: el.offsetWidth - el.clientWidth,
      scrolls: el.scrollWidth > el.clientWidth,
    };
  });

for (const [name, viewport] of [
  ["desktop", { width: 1000, height: 800 }],
  ["phone", { width: 390, height: 844 }],
]) {
  test(`the tab row has no scrollbar (${name})`, async ({ page, context }) => {
    await page.setViewportSize(viewport);
    await setup(context);
    await routeDashboards(context);
    await page.goto(WATCH);
    await expect(page.locator("[data-watch-more-dashboards]")).toBeVisible();
    await expect(
      page.locator('[data-watch-tab="dashboard:billing"]'),
    ).toBeAttached();
    const box = await tabBox(page);
    expect(box).toMatchObject({
      overflowY: "hidden",
      scrollbarWidth: "none",
      verticalOverflow: 0,
      scrollbarX: 0,
      scrollbarY: 0,
    });
    // Tabs that do not fit scroll sideways, and the page itself does not.
    expect(box.scrolls).toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
    // Picking the last tab brings it into view.
    await page.locator('[data-watch-tab="dashboard:billing"]').click();
    await expect(
      page.locator('[data-watch-tab="dashboard:billing"]'),
    ).toBeInViewport({ ratio: 0.9 });
    if (process.env.WATCH_SHOTS)
      await page.screenshot({
        path: `${process.env.WATCH_SHOTS}/watch-tabs-${name}.png`,
        clip: { x: 0, y: 0, width: viewport.width, height: 220 },
      });
  });
}

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("a tap opens a row and the next tap closes it", async ({
    page,
    context,
  }) => {
    await setup(context, (route) => json(route, briefWithRollup()));
    await routeObservabilityWork(context);
    await page.goto(WATCH);
    const row = page.locator('[data-collection-row="card:observability"]');
    await row
      .locator(".grid-row")
      .first()
      .tap({ position: { x: 200, y: 12 } });
    const details = row.locator("[data-collection-details]");
    await expect(details).toBeVisible();
    await expect(details.locator("[data-details-steps]")).toContainText(
      "Bake the m6 image",
    );
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
    if (process.env.WATCH_SHOTS)
      await page.screenshot({
        path: `${process.env.WATCH_SHOTS}/watch-row-expanded-phone.png`,
        fullPage: true,
      });
    await row.locator("[data-collection-toggle]").tap();
    await expect(details).toHaveCount(0);
  });

  test("Lanes stack on a phone", async ({ page, context }) => {
    await setup(context, (route) => json(route, briefWithRollup()));
    await context.route(
      (url) => url.pathname === "/work/ungrouped",
      (route) => json(route, { work: [], has_more: false }),
    );
    await page.goto(`${WATCH}?view=lanes`);
    await expect(
      page.locator('[data-lane-ref="card:fleet-onboarding"]'),
    ).toBeVisible();
    await expect(page.locator("[data-initiative-budget]")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
    if (process.env.WATCH_SHOTS)
      await page.screenshot({
        path: `${process.env.WATCH_SHOTS}/watch-lanes-phone.png`,
        fullPage: true,
      });
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
  await climber.locator("[data-collection-toggle]").click();
  const card = climber.locator('[data-loop="card:avo-cleanup"]');
  await expect(card.locator("[data-loop-chart] polyline")).toHaveCount(2);
  await expect(card.locator("[data-loop-runs] li")).toHaveCount(6);
  await expect(card).toContainText("reclaimable_gb");
  await row("card:retention-check").locator("[data-collection-toggle]").click();
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
