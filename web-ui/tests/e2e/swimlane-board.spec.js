import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { expectCleanLayout } from "../helpers/layoutAudit.js";
import { setup } from "./helpers/durable-discussion.js";

/**
 * The swimlane board, Watch's Lanes view (`/watch?view=lanes`): lanes from
 * the brief's rollups, the Ungrouped tray, grouping and folding through the
 * device outbox, and every state, at desktop and phone widths.
 *
 * `BOARD_SHOTS=<dir>` also writes the screenshots used in the PR.
 */
const LANES = "/o/local/w/local/watch?view=lanes";
const NOW = "2026-10-24T12:00:00Z";

const json = (route, value, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(value),
  });

const child = (ref, title, state, extra = {}) => ({
  ref,
  title,
  step_id: `card-${ref.slice(5)}`,
  state,
  past_promise: false,
  needs_viewer: false,
  ...extra,
});

function rollup(children, extra = {}) {
  const by_state = {};
  for (const row of children)
    by_state[row.state] = (by_state[row.state] || 0) + 1;
  return {
    total: children.length,
    by_state,
    past_promise: children.filter((row) => row.past_promise).length,
    needing_viewer: children.filter((row) => row.needs_viewer).length,
    workstreams: [],
    children,
    has_more: false,
    ...extra,
  };
}

function lane(ref, title, children, extra = {}) {
  return {
    ref,
    title,
    state: "moving",
    placement: "in_progress",
    parent_ref: null,
    blocked_count: 0,
    waiting_on_you_count: 0,
    rollup: rollup(children, extra.rollup),
    ...extra,
    ...(extra.rollup ? { rollup: rollup(children, extra.rollup) } : {}),
  };
}

function seed() {
  const capture = [
    child("card:japanese-stt", "Japanese on-device STT", "backlog"),
    child(
      "card:record-lane",
      "Record lane and legacy retirement",
      "in_progress",
    ),
    child("card:custody", "Custody and merging", "in_progress"),
    child("card:speaker-id", "Speaker identification", "review", {
      past_promise: true,
    }),
    child("card:a1-funding", "A1 fallback funding ruling", "blocked", {
      needs_viewer: true,
    }),
    child("card:fail-loudly", "Fail loudly", "done"),
  ];
  const margin = [
    child("card:storage", "Storage levers", "ready"),
    ...[
      "Firestore reads",
      "GPU sizing",
      "Pricing ladder",
      "Billing settle",
      "Batch profiler",
      "Egress audit",
    ].map((title, index) =>
      child(`card:margin-${index}`, title, "in_progress"),
    ),
    child("card:pro-price", "Pro price: $49.99 or $29.99?", "blocked", {
      needs_viewer: true,
    }),
  ];
  return {
    brief: {
      schema: "brief.v1",
      generated_at: NOW,
      principal: { id: "actor-comment", display: "Reader", kind: "human" },
      decisions: [],
      initiatives: [
        lane("card:retention", "A product users keep using", []),
        lane("card:margin", "Unit economics: Margin-50", margin, {
          state: "past_promise",
          rollup: { total: 11 },
        }),
        lane("card:capture", "Every word captured and attributed", capture, {
          rollup: { workstreams: [capture[2]] },
        }),
        lane("card:p76-coldline", "P76 coldline storage", [], {
          state: "no_promise",
          placement: "backlog",
        }),
        lane(
          "card:security",
          "Security and trust",
          [child("card:keyless", "Keyless program", "done")],
          { state: "done", placement: "done" },
        ),
      ],
      loops: [
        lane("card:signup-loop", "Signup conversion loop", [], {
          loop: { health: "healthy" },
        }),
      ],
      initiative_budget: { count: 5, target: 15, truncated: false },
      next_actions: [],
      your_asks: [],
      answered_for_you: [],
      truncated: { initiatives: false, loops: false },
    },
    ungrouped: [
      {
        ref: "card:a1-f4",
        title: "A1 F4: retry budget for funded failover",
        phase: "backlog",
        parent_ref: null,
        created_at: "2026-10-24T11:58:00Z",
        likely_home: [
          {
            ref: "card:capture",
            title: "Every word captured and attributed",
            reasons: ["title_family"],
          },
        ],
        next_actions: [],
      },
      {
        ref: "card:cost-4",
        title: "Cost 4/5: Railway egress",
        phase: "backlog",
        parent_ref: null,
        created_at: "2026-10-24T11:00:00Z",
        likely_home: [
          {
            ref: "card:margin",
            title: "Unit economics: Margin-50",
            reasons: ["title_family", "same_repository"],
          },
        ],
        next_actions: [],
      },
      {
        ref: "card:ramp-telemetry",
        title: "Shared-speech ramp telemetry",
        phase: "backlog",
        parent_ref: null,
        created_at: "2026-10-24T09:00:00Z",
        likely_home: [],
        next_actions: [],
      },
    ],
    hasMore: false,
    plans: {},
    planTokens: {},
    planReceipts: {},
    planWrites: 0,
    losePlanResponses: 0,
    dropPlanWrites: 0,
    calls: [],
    posts: [],
    hold: {},
    fail: {},
  };
}

/** Moves a tray card into its new lane, as core's next reads would show it. */
function landChild(api, ref, under) {
  const row = api.ungrouped.find((entry) => entry.ref === ref);
  api.ungrouped = api.ungrouped.filter((entry) => entry.ref !== ref);
  const target = api.brief.initiatives.find((entry) => entry.ref === under);
  const children = [
    ...target.rollup.children,
    child(ref, row?.title || ref, row?.phase || "backlog"),
  ];
  target.rollup = rollup(children);
  api.plans[under] = {
    steps: [
      ...(api.plans[under]?.steps ?? []),
      { id: `card-${ref.slice(5)}`, title: row?.title, ref, after: [] },
    ],
  };
  return row;
}

async function install(context, api) {
  await setup(context, () => {});
  await context.route(
    /\/(?:stream\/(?:events|inbox)|inbox\/stream)(?:\?.*)?$/,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "",
      }),
  );
  await context.route(/\/work(?:\?.*)?$/, (route) => {
    api.calls.push("GET /work");
    return json(route, { work: [] });
  });
  await context.route(
    (url) => url.pathname === "/brief",
    async (route) => {
      api.calls.push("GET /brief");
      if (api.hold.brief) await api.hold.brief;
      if (api.fail.brief)
        return json(route, { error: { message: "Brief failed" } }, 500);
      return json(route, api.brief);
    },
  );
  await context.route(
    (url) => url.pathname === "/work/ungrouped",
    async (route) => {
      api.calls.push(
        `GET /work/ungrouped?${new URL(route.request().url()).searchParams}`,
      );
      if (api.fail.ungrouped)
        return json(route, { error: { message: "Tray failed" } }, 500);
      return json(route, { work: api.ungrouped, has_more: api.hasMore });
    },
  );
  await context.route(
    (url) =>
      /^\/work\/[^/]+\/(group|fold|unfold)$/.test(
        decodeURIComponent(url.pathname),
      ),
    async (route) => {
      const path = decodeURIComponent(new URL(route.request().url()).pathname);
      const [, ref, verb] = path.match(
        /^\/work\/([^/]+)\/(group|fold|unfold)$/,
      );
      const body = route.request().postDataJSON();
      api.posts.push({ verb, ref, body });
      if (api.fail[verb]) {
        const failure = api.fail[verb];
        if (typeof failure === "number" && failure > 0) api.fail[verb] -= 1;
        return json(route, { error: { message: "Upstream detail" } }, 503);
      }
      if (verb === "group") landChild(api, ref, body.under);
      if (verb === "fold") {
        api.brief.initiatives = api.brief.initiatives.filter(
          (entry) => entry.ref !== ref,
        );
        api.brief.next_actions = [];
      }
      return json(route, {
        work: { ref, parent_ref: body.under || body.into || null },
      });
    },
  );
  await context.route(
    (url) => /^\/cards\/[^/]+\/plan$/.test(decodeURIComponent(url.pathname)),
    async (route) => {
      const path = decodeURIComponent(new URL(route.request().url()).pathname);
      const ref = path.split("/")[2];
      const method = route.request().method();
      api.calls.push(`${method} ${path}`);
      const token = () => api.planTokens[ref] ?? "2026-10-24T12:00:01Z";
      if (method === "GET")
        return json(route, {
          card_ref: ref,
          plan: api.plans[ref] ?? { steps: [] },
          if_updated_at: token(),
        });
      // Core's keyed plan write: a known key replays its original response
      // or refuses another body; a stale token is refused.
      const body = route.request().postDataJSON();
      api.posts.push({ verb: "plan", ref, body });
      const seen = api.planReceipts[body.request_key];
      if (seen)
        return JSON.stringify(seen.body) === JSON.stringify(body)
          ? json(route, seen.response)
          : json(route, { error: { message: "key reused" } }, 409);
      if (api.dropPlanWrites > 0) {
        api.dropPlanWrites -= 1;
        return json(route, { error: { message: "gateway timeout" } }, 503);
      }
      if (body.if_updated_at !== token())
        return json(route, { error: { message: "plan changed" } }, 409);
      api.plans[ref] = body.plan;
      api.planTokens[ref] = `2026-10-24T12:00:0${++api.planWrites + 1}Z`;
      const response = {
        card_ref: ref,
        plan: body.plan,
        if_updated_at: api.planTokens[ref],
      };
      api.planReceipts[body.request_key] = { body, response };
      if (api.losePlanResponses > 0) {
        api.losePlanResponses -= 1;
        return json(route, { error: { message: "gateway timeout" } }, 503);
      }
      return json(route, response);
    },
  );
}

async function open(page, context, api = seed(), { fixTime = true } = {}) {
  // A fixed clock also freezes the outbox's retry schedule, so the
  // durability case runs on real time.
  if (fixTime) await page.clock.setFixedTime(new Date(NOW));
  await install(context, api);
  await page.goto(LANES);
  return api;
}

const laneLocator = (page, ref) => page.locator(`[data-lane-ref="${ref}"]`);

/** `aria-controls` values on the board that name no element. */
const danglingControls = (page) =>
  page.evaluate(() =>
    [...document.querySelectorAll("[data-swimlane-board] [aria-controls]")]
      .map((node) => node.getAttribute("aria-controls"))
      .filter((id) => !document.getElementById(id)),
  );

/** Element ids used more than once in the document. */
const duplicateIds = (page) =>
  page.evaluate(() => {
    const seen = new Map();
    for (const node of document.querySelectorAll("[id]"))
      seen.set(node.id, (seen.get(node.id) ?? 0) + 1);
    return [...seen].filter(([, n]) => n > 1).map(([id]) => id);
  });

async function shot(page, name) {
  if (!process.env.BOARD_SHOTS) return;
  await page.screenshot({
    path: `${process.env.BOARD_SHOTS}/${name}.png`,
    fullPage: true,
  });
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("lanes come from the brief's rollups, with no per-lane reads", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    const lanes = page.locator("[data-lane-ref]");
    // Needs-you first, then past promise, then core order, done last.
    await expect(lanes).toHaveCount(5);
    expect(
      await lanes.evaluateAll((rows) =>
        rows.map((row) => row.getAttribute("data-lane-ref")),
      ),
    ).toEqual([
      // Both need the reader; Margin is also past its own promise.
      "card:margin",
      "card:capture",
      "card:retention",
      "card:p76-coldline",
      "card:security",
    ]);

    await expect(page.locator("[data-swimlane-tray]")).toBeVisible();
    await shot(page, "lanes-desktop");
    const capture = laneLocator(page, "card:capture");
    await expect(
      capture.locator(
        '[data-lane-column="stuck"] [data-work-ref="card:a1-funding"]',
      ),
    ).toContainText("you");
    await expect(
      capture.locator(
        '[data-lane-column="moving"] [data-work-ref="card:speaker-id"]',
      ),
    ).toContainText("past promise");
    await expect(
      capture.locator(
        '[data-lane-column="moving"] [data-work-ref="card:custody"]',
      ),
    ).toContainText("workstream");
    await expect(
      capture.locator(
        '[data-lane-column="not_started"] [data-work-ref="card:japanese-stt"]',
      ),
    ).toBeVisible();
    await expect(
      capture.locator(
        '[data-lane-column="done"] [data-work-ref="card:fail-loudly"]',
      ),
    ).toBeVisible();
    await expect(
      capture.locator(
        '[data-source="computed:initiatives.rollup.needing_viewer"]',
      ),
    ).toHaveText("1 need you");
    await expect(
      capture.locator(
        '[data-source="computed:initiatives.rollup.past_promise"]',
      ),
    ).toHaveText("1 past promise");
    await expect(capture.locator("[data-lane-progress]")).toHaveAttribute(
      "aria-label",
      "1 done, 3 in progress, 1 blocked or needs you, 1 not started",
    );

    // A large lane: five cards per cell, then the rest on request, and the
    // children core did not preview link to the initiative.
    const margin = laneLocator(page, "card:margin");
    const busy = margin.locator('[data-lane-column="moving"]');
    await expect(busy.locator("[data-work-ref]")).toHaveCount(5);
    await busy.getByRole("button", { name: "+ 1 more" }).click();
    await expect(busy.locator("[data-work-ref]")).toHaveCount(6);
    await expect(margin.locator("[data-lane-more]")).toHaveText(
      "3 more in this initiative →",
    );

    // Done initiatives start collapsed and still show column counts.
    await expect(laneLocator(page, "card:security")).toHaveAttribute(
      "data-lane-collapsed",
      "true",
    );
    await capture.locator("[data-lane-toggle]").click();
    await expect(capture).toHaveAttribute("data-lane-collapsed", "true");
    // Collapsed lanes and the closed Loops list point at nothing missing.
    expect(await danglingControls(page)).toEqual([]);
    await expect(capture.locator("[data-work-ref]")).toHaveCount(0);
    await capture.locator("[data-lane-toggle]").click();

    // Loops get their own section, not lanes.
    await expect(laneLocator(page, "card:signup-loop")).toHaveCount(0);
    await page.locator("[data-swimlane-loops] button").click();
    await expect(page.locator("[data-swimlane-loops]")).toContainText(
      "Signup conversion loop",
    );

    await expect(page.locator("[data-initiative-budget]")).toHaveText(
      "Initiatives 5/15",
    );
    await expect(page.locator("[data-swimlane-tray]")).toContainText(
      "Ungrouped · 3",
    );
    // One brief and one tray page; nothing per lane, and no work list.
    expect(api.calls.filter((call) => call.startsWith("GET /cards"))).toEqual(
      [],
    );
    expect(api.calls).not.toContain("GET /work");
    expect(
      api.calls.filter((call) => call.startsWith("GET /work/ungrouped")),
    ).toEqual(["GET /work/ungrouped?limit=20"]);

    await expectCleanLayout(page, "swimlane board desktop");
    const axe = await new AxeBuilder({ page })
      .include("[data-swimlane-board]")
      .analyze();
    expect(axe.violations).toEqual([]);
  });

  test("one tap groups under a likely home, and Undo takes the step back out", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    const row = page.locator('[data-tray-ref="card:a1-f4"]');
    await expect(row).toContainText(
      "Likely home: Every word captured and attributed · same title prefix",
    );
    await row.locator('[data-group-under="card:capture"]').click();
    await expect(page.locator("[data-grouping-notice]")).toContainText(
      "Grouped “A1 F4: retry budget for funded failover” under “Every word captured and attributed”.",
    );
    const [sent] = api.posts;
    expect(sent).toMatchObject({
      verb: "group",
      ref: "card:a1-f4",
      body: { under: "card:capture", actor_id: "actor-comment" },
    });
    expect(sent.body.request_key).toMatch(/^[0-9a-f-]{36}$/);
    // Core's next reads show it in the lane and out of the tray.
    await expect(
      laneLocator(page, "card:capture").locator('[data-work-ref="card:a1-f4"]'),
    ).toBeVisible();
    await expect(page.locator('[data-tray-ref="card:a1-f4"]')).toHaveCount(0);
    await shot(page, "lanes-grouped-undo");

    await page
      .locator("[data-grouping-notice]")
      .getByRole("button", { name: "Undo" })
      .click();
    await expect(page.locator("[data-grouping-notice]")).toContainText(
      "It is back in Ungrouped.",
    );
    const plan = api.posts.find((post) => post.verb === "plan");
    expect(plan.ref).toBe("card:capture");
    expect(plan.body.plan.steps.map((step) => step.ref)).not.toContain(
      "card:a1-f4",
    );
    expect(plan.body.if_updated_at).toBe("2026-10-24T12:00:01Z");
    // The undo's own key, not one derived from a later read.
    expect(plan.body.request_key).toMatch(/^[0-9a-f-]{36}$/);
  });

  test("an undo whose response was lost never removes a step someone re-added", async ({
    page,
    context,
  }) => {
    // The review's sequence: the undo's plan write commits but its response
    // is lost; another operator groups the card again; the retry fires.
    const api = await open(page, context, seed(), { fixTime: false });
    await page
      .locator('[data-tray-ref="card:a1-f4"]')
      .locator('[data-group-under="card:capture"]')
      .click();
    const notice = page.locator("[data-grouping-notice]");
    await expect(notice).toContainText("Grouped “A1 F4");
    api.losePlanResponses = 1;
    await notice.getByRole("button", { name: "Undo" }).click();
    await expect
      .poll(() => api.posts.filter((post) => post.verb === "plan").length)
      .toBe(1);
    await expect(
      page.locator('[data-mutation-state="retrying"]'),
    ).toBeVisible();
    // Another operator puts the step back before the retry.
    api.plans["card:capture"] = {
      steps: [
        ...api.plans["card:capture"].steps,
        { id: "card-a1-f4", title: "A1 F4", ref: "card:a1-f4", after: [] },
      ],
    };
    api.planTokens["card:capture"] = "2026-10-24T12:30:00Z";
    await expect
      .poll(() => api.posts.filter((post) => post.verb === "plan").length, {
        timeout: 15_000,
      })
      .toBe(2);
    const [first, retry] = api.posts.filter((post) => post.verb === "plan");
    // The retry replays the first write byte for byte: same key, same body.
    expect(retry.body).toEqual(first.body);
    await expect(notice).toContainText("It is back in Ungrouped.");
    // The other operator's step survives.
    expect(api.plans["card:capture"].steps.map((step) => step.ref)).toContain(
      "card:a1-f4",
    );
    // Only one plan read: the retry never re-derived its write.
    expect(
      api.calls.filter((call) => call === "GET /cards/card:capture/plan"),
    ).toHaveLength(1);
  });

  test("an undo that never landed is refused once the plan moves on", async ({
    page,
    context,
  }) => {
    const api = await open(page, context, seed(), { fixTime: false });
    await page
      .locator('[data-tray-ref="card:a1-f4"]')
      .locator('[data-group-under="card:capture"]')
      .click();
    const notice = page.locator("[data-grouping-notice]");
    await expect(notice).toContainText("Grouped “A1 F4");
    api.dropPlanWrites = 1;
    await notice.getByRole("button", { name: "Undo" }).click();
    await expect(
      page.locator('[data-mutation-state="retrying"]'),
    ).toBeVisible();
    // Someone edits the plan before the retry.
    api.planTokens["card:capture"] = "2026-10-24T12:30:00Z";
    await expect(
      page.locator('[data-mutation-state="attention"]'),
    ).toContainText("nothing was removed", { timeout: 15_000 });
    const writes = api.posts.filter((post) => post.verb === "plan");
    expect(writes).toHaveLength(2);
    expect(writes[1].body).toEqual(writes[0].body);
    expect(api.plans["card:capture"].steps.map((step) => step.ref)).toContain(
      "card:a1-f4",
    );
  });

  test("dragging a tray card onto a lane groups it there", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    const source = page.locator('[data-tray-ref="card:ramp-telemetry"]');
    await expect(source).toContainText("No likely home");
    const from = await source.locator("span").first().boundingBox();
    const to = await laneLocator(page, "card:margin").boundingBox();
    await page.mouse.move(from.x + 10, from.y + 8);
    await page.mouse.down();
    await page.mouse.move(from.x + 40, from.y + 40, { steps: 4 });
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, {
      steps: 8,
    });
    await expect(page.locator("[data-swimlane-drag]")).toContainText(
      "Group under Unit economics: Margin-50",
    );
    await page.mouse.up();
    await expect
      .poll(() =>
        api.posts.map((post) => [post.verb, post.ref, post.body.under]),
      )
      .toEqual([["group", "card:ramp-telemetry", "card:margin"]]);
    await expect(
      laneLocator(page, "card:margin").locator(
        '[data-work-ref="card:ramp-telemetry"]',
      ),
    ).toBeVisible();
  });

  test("an uncertain group survives reload and resends its original key", async ({
    page,
    context,
  }) => {
    const api = seed();
    api.fail.group = 1;
    await open(page, context, api, { fixTime: false });
    await page
      .locator('[data-tray-ref="card:cost-4"]')
      .locator('[data-group-under="card:margin"]')
      .click();
    await expect(
      page.locator('[data-mutation-state="retrying"]'),
    ).toBeVisible();
    await expect(page.getByText("Upstream detail")).not.toBeVisible();
    await expect(page.locator('[data-tray-ref="card:cost-4"]')).toContainText(
      "Grouping saved. Sending…",
    );
    await page.reload();
    await expect
      .poll(() => api.posts.filter((post) => post.verb === "group").length, {
        timeout: 15_000,
      })
      .toBe(2);
    const [first, second] = api.posts;
    expect(second.body).toEqual(first.body);
    await expect(
      laneLocator(page, "card:margin").locator('[data-work-ref="card:cost-4"]'),
    ).toBeVisible();
  });

  test("over budget suggests folds, and a fold can be undone", async ({
    page,
    context,
  }) => {
    const api = seed();
    api.brief.initiative_budget = { count: 17, target: 15, truncated: true };
    api.brief.next_actions = [
      {
        label: "Choose a home",
        argv: [
          "anx",
          "work",
          "fold",
          "card:p76-coldline",
          "--into",
          "card:margin",
        ],
        mutates: true,
        reasons: ["title_family"],
      },
    ];
    await open(page, context, api);
    const chip = page.locator("[data-initiative-budget]");
    await expect(chip).toHaveText("Initiatives 17+/15 · suggest folds");
    await chip.click();
    const panel = page.locator("[data-fold-suggestions]");
    await expect(panel).toContainText(
      "“P76 coldline storage” into “Unit economics: Margin-50” · same title prefix",
    );
    await shot(page, "lanes-fold-suggestions");
    await panel.locator('[data-fold="card:p76-coldline"]').click();
    await expect(page.locator("[data-grouping-notice]")).toContainText(
      "Folded “P76 coldline storage” into “Unit economics: Margin-50”.",
    );
    expect(api.posts[0]).toMatchObject({
      verb: "fold",
      ref: "card:p76-coldline",
      body: { into: "card:margin" },
    });
    await expect(laneLocator(page, "card:p76-coldline")).toHaveCount(0);
    await page
      .locator("[data-grouping-notice]")
      .getByRole("button", { name: "Undo" })
      .click();
    await expect
      .poll(() => api.posts.map((post) => post.verb))
      .toEqual(["fold", "unfold"]);
    expect(api.posts[1].body.request_key).not.toBe(
      api.posts[0].body.request_key,
    );
  });

  test("loading, empty, all grouped and failures each say what is going on", async ({
    page,
    context,
  }) => {
    const api = seed();
    let release;
    api.hold.brief = new Promise((resolve) => {
      release = resolve;
    });
    await open(page, context, api);
    await expect(page.locator("[data-swimlanes-loading]")).toBeVisible();
    await shot(page, "lanes-loading");
    // All grouped: no tray at all.
    api.ungrouped = [];
    release();
    await expect(page.locator("[data-lane-ref]").first()).toBeVisible();
    await expect(page.locator("[data-swimlane-tray]")).toHaveCount(0);

    // Empty: no initiatives, cards waiting.
    api.hold.brief = null;
    api.brief.initiatives = [];
    api.brief.loops = [];
    api.ungrouped = seed().ungrouped;
    await page.reload();
    await expect(page.locator("[data-swimlanes-empty]")).toContainText(
      "Cards are waiting in Ungrouped. Create an initiative to group them under.",
    );
    await shot(page, "lanes-empty");

    // A failed refresh keeps the board it already had and says so.
    api.fail.brief = true;
    await page.reload();
    await expect(page.locator("[data-swimlanes-empty]")).toBeVisible();
    await expect(
      page.getByText(
        "Showing the board as last loaded. It may be out of date.",
      ),
    ).toBeVisible();
    api.fail.brief = false;
    api.brief = seed().brief;
    api.fail.ungrouped = true;
    await page.reload();
    await expect(page.locator("[data-lane-ref]").first()).toBeVisible();
    await expect(
      page
        .locator("[data-swimlane-tray]")
        .getByText("Ungrouped cards didn’t load"),
    ).toBeVisible();
    await expect(page.getByText("Tray failed", { exact: true })).toHaveCount(0);
    // A tray that never loaded has no count to show.
    await expect(page.locator("#swimlane-tray-title")).not.toContainText("· 0");
    await shot(page, "lanes-tray-error");
  });
});

test("old links land on the one board, in Watch", async ({ page, context }) => {
  await install(context, seed());
  // The Initiatives page's Lanes view moved here.
  await page.goto("/o/local/w/local/initiatives?view=lanes");
  await expect(page).toHaveURL(/\/watch\?view=lanes$/);
  await expect(page.locator("[data-lane-ref]").first()).toBeVisible();
  await expect(page.locator('[data-watch-view="lanes"]')).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  // The by-stage board's links open the lanes, which replaced it.
  for (const query of ["view=board", "layout=board"]) {
    await page.goto(`/o/local/w/local/watch?${query}`);
    await expect(page.locator("[data-lane-ref]").first()).toBeVisible();
  }
  // Initiatives keeps Table and By placement; Lanes links to Watch.
  await page.goto("/o/local/w/local/initiatives");
  await expect(page.locator("[data-view-lanes]")).toHaveAttribute(
    "href",
    /\/watch\?view=lanes$/,
  );
});

test("a first read that fails shows one banner and no raw body", async ({
  page,
  context,
}) => {
  const api = seed();
  api.fail.brief = true;
  await open(page, context, api);
  await expect(page.getByText("The board didn’t load")).toBeVisible();
  await expect(page.getByText("Brief failed", { exact: true })).toHaveCount(0);
  await shot(page, "lanes-error");
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("lanes stack as sections and the tray opens as a sheet", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    const capture = laneLocator(page, "card:capture");
    // Lanes that need the reader start open; quiet ones start folded.
    await expect(capture).toHaveAttribute("data-lane-collapsed", "false");
    await expect(laneLocator(page, "card:retention")).toHaveAttribute(
      "data-lane-collapsed",
      "true",
    );
    await expect(
      capture.locator('[data-work-ref="card:a1-funding"]'),
    ).toBeVisible();
    // Empty columns are not drawn on a phone.
    await expect(
      laneLocator(page, "card:margin").locator('[data-lane-column="done"]'),
    ).toBeHidden();
    await expect(page.locator("[data-swimlane-tray]")).toBeHidden();
    await shot(page, "lanes-phone");
    await expectCleanLayout(page, "swimlane board phone");

    await page.locator("[data-swimlane-tray-open]").tap();
    const sheet = page.getByRole("dialog", { name: /Ungrouped/ });
    await expect(sheet).toBeVisible();
    // The sheet's controls are its own: no id is used twice.
    expect(await duplicateIds(page)).toEqual([]);
    await shot(page, "lanes-phone-tray");
    await sheet.locator('[data-group-under="card:capture"]').tap();
    await expect
      .poll(() => api.posts.map((post) => [post.verb, post.ref]))
      .toEqual([["group", "card:a1-f4"]]);
    await sheet.getByRole("button", { name: "Done" }).tap();
    await expect(sheet).toHaveCount(0);
    await expect(capture.locator('[data-work-ref="card:a1-f4"]')).toBeVisible();
  });
});
