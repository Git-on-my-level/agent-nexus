import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { expectCleanLayout } from "../helpers/layoutAudit.js";
import { setup } from "./helpers/durable-discussion.js";

/**
 * The swimlane board, Watch's Lanes view (`/watch?view=lanes`): lanes from
 * the brief's rollups, the Unsorted tray, grouping and folding through the
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
    child("card:fail-loudly", "Fail loudly", "done", {
      done_at: "2026-10-23T09:00:00Z",
    }),
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
        needs_home: true,
        created_at: "2026-10-24T11:58:00Z",
        created_by: { id: "agent:lane-stt", display: "lane-stt" },
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
        needs_home: true,
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
        needs_home: true,
        created_at: "2026-10-24T09:00:00Z",
        likely_home: [],
        next_actions: [
          {
            label: "Make it an initiative",
            command_id: "work.move",
            argv: [
              "anx",
              "work",
              "move",
              "card:ramp-telemetry",
              "--root",
              "--reason",
              "An independent outcome",
            ],
          },
          {
            label: "Move to another workspace",
            command_template:
              "anx move card card:ramp-telemetry --to <workspace-alias>",
            requires_human: true,
            requires_destination: true,
          },
        ],
      },
    ],
    moveReceipts: {},
    moveEffects: 0,
    loseMoveResponses: 0,
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
      /^\/work\/[^/]+\/(move|group|fold|unfold)$/.test(
        decodeURIComponent(url.pathname),
      ),
    async (route) => {
      const path = decodeURIComponent(new URL(route.request().url()).pathname);
      const [, ref, verb] = path.match(
        /^\/work\/([^/]+)\/(move|group|fold|unfold)$/,
      );
      const body = route.request().postDataJSON();
      api.posts.push({
        verb,
        ref,
        body,
        key: body.request_key,
        headerKey: route.request().headers()["idempotency-key"],
      });
      if (api.fail[verb]) {
        const failure = api.fail[verb];
        if (typeof failure === "number" && failure > 0) api.fail[verb] -= 1;
        return json(route, { error: { message: "Upstream detail" } }, 503);
      }
      const replayKey = `${ref}:${body.actor_id}:${body.request_key}`;
      if (verb === "move") {
        if (!body.request_key || !body.actor_id)
          return json(
            route,
            { error: { message: "Missing replay identity" } },
            400,
          );
        const saved = api.moveReceipts[replayKey];
        if (saved)
          return JSON.stringify(saved.body) === JSON.stringify(body)
            ? json(route, saved.response)
            : json(route, { error: { message: "Key reused" } }, 409);
        api.moveEffects += 1;

        if (body.under) landChild(api, ref, body.under);
        else if (body.root) {
          const row = api.ungrouped.find((entry) => entry.ref === ref);
          api.ungrouped = api.ungrouped.filter((entry) => entry.ref !== ref);
          api.brief.initiatives.push(lane(ref, row?.title || ref, []));
        } else if (body.unsorted) {
          let row;
          for (const parent of api.brief.initiatives) {
            row ||= parent.rollup.children.find((entry) => entry.ref === ref);
            parent.rollup = rollup(
              parent.rollup.children.filter((entry) => entry.ref !== ref),
            );
          }
          const initiative = api.brief.initiatives.find(
            (entry) => entry.ref === ref,
          );
          row ||= initiative;
          api.brief.initiatives = api.brief.initiatives.filter(
            (entry) => entry.ref !== ref,
          );
          api.ungrouped.push({
            ref,
            title: row?.title || ref,
            needs_home: true,
            parent_ref: null,
          });
        }
      }
      if (verb === "group") landChild(api, ref, body.under);
      if (verb === "fold") {
        api.brief.initiatives = api.brief.initiatives.filter(
          (entry) => entry.ref !== ref,
        );
        api.brief.next_actions = [];
      }
      const response = {
        work: { ref, parent_ref: body.under || body.into || null },
      };
      if (verb === "move") {
        api.moveReceipts[replayKey] = { body, response };
        if (api.loseMoveResponses > 0) {
          api.loseMoveResponses -= 1;
          return json(route, { error: { message: "Gateway timeout" } }, 503);
        }
      }
      return json(route, response);
    },
  );
  // Core rejects the removed field; legacy queued intents must use move.
  await context.route(
    (url) =>
      /^\/work\/[^/]+$/.test(url.pathname) &&
      url.pathname !== "/work/ungrouped",
    async (route) => {
      api.posts.push({ verb: "patch", body: route.request().postDataJSON() });
      return json(
        route,
        { error: { message: "top_level is replaced by the work tree" } },
        400,
      );
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
    await expect(capture.locator("[data-lane-needs]")).toHaveText("1 need you");
    await expect(capture.locator("[data-lane-slipping]")).toHaveText(
      "1 slipping",
    );
    // One segment per child, coloured by where it sits, and said in words.
    await expect(capture.locator("[data-lane-progress]")).toHaveAttribute(
      "aria-label",
      "1 done, 2 in progress, 1 slipping, 1 need you, 1 not started",
    );
    await expect(capture.locator("[data-lane-progress] > span")).toHaveCount(6);

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
      "Unsorted · 3",
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

  test("one tap moves under a suggested home without changing a plan", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    await page
      .locator('[data-tray-ref="card:a1-f4"] [data-group-under="card:capture"]')
      .click();
    await expect(page.locator("[data-grouping-notice]")).toContainText(
      "Confirmed.",
    );
    expect(api.posts[0]).toMatchObject({
      verb: "move",
      ref: "card:a1-f4",
      body: { under: "card:capture" },
    });
    expect(api.posts[0].key).toMatch(/^[0-9a-f-]{36}$/);
    expect(api.posts[0].body.actor_id).toBe("actor-comment");
    expect(Object.keys(api.posts[0].body).sort()).toEqual([
      "actor_id",
      "request_key",
      "under",
    ]);
    expect(api.posts[0].headerKey).toBeUndefined();
    expect(api.plans).toEqual({});
    await expect(page.locator('[data-tray-ref="card:a1-f4"]')).toHaveCount(0);
    await expect(
      laneLocator(page, "card:capture").locator('[data-work-ref="card:a1-f4"]'),
    ).toBeVisible();
  });

  test("move to top level collects and persists a reason", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    await page.locator('[data-promote="card:ramp-telemetry"]').click();
    const draft = page.locator("[data-saved-grouping]");
    await expect(draft.getByLabel("Reason")).toHaveValue(
      "An independent outcome",
    );
    await draft.getByLabel("Reason").fill("A separate measurable outcome");
    await page.reload();
    await expect(draft.getByLabel("Reason")).toHaveValue(
      "A separate measurable outcome",
    );
    await draft.getByRole("button", { name: "Save move" }).click();
    await expect(page.locator("[data-grouping-notice]")).toContainText(
      "Confirmed.",
    );
    expect(api.posts[0]).toMatchObject({
      verb: "move",
      ref: "card:ramp-telemetry",
      body: { root: true, reason: "A separate measurable outcome" },
    });
  });

  test("a child can move to Unsorted through a saved move", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    await page
      .getByRole("button", { name: "Move Japanese on-device STT", exact: true })
      .click();
    const draft = page.locator("[data-saved-grouping]");
    await expect(draft.getByLabel("Destination")).toHaveValue("unsorted");
    await draft.getByRole("button", { name: "Save move" }).click();
    await expect(page.locator("[data-grouping-notice]")).toContainText(
      "Confirmed.",
    );
    expect(api.posts[0]).toMatchObject({
      verb: "move",
      ref: "card:japanese-stt",
      body: { unsorted: true },
    });
    await expect(
      page.locator('[data-tray-ref="card:japanese-stt"]'),
    ).toBeVisible();
  });

  test("Move to another workspace shows the command and sends nothing", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    const row = page.locator(
      '[data-swimlane-tray] [data-tray-ref="card:ramp-telemetry"]',
    );
    await row.locator("select").selectOption("+move");
    const move = row.locator("[data-tray-move]");
    await expect(move).toContainText(
      "anx move card card:ramp-telemetry --to <workspace-alias>",
    );
    await expect(move).toContainText("A person with access to both");
    expect(api.posts).toEqual([]);
    await move.getByRole("button", { name: "Close" }).click();
    await expect(move).toHaveCount(0);
    // A card with no move hint from core offers none.
    await expect(
      page
        .locator('[data-swimlane-tray] [data-tray-ref="card:a1-f4"] option')
        .filter({ hasText: "Move to another workspace" }),
    ).toHaveCount(0);
  });

  test("dragging a tray card onto a lane groups it there", async ({
    page,
    context,
  }) => {
    const api = await open(page, context);
    const source = page.locator('[data-tray-ref="card:ramp-telemetry"]');
    await expect(source).toContainText("no likely home");
    // The tray sits under the board: bring it up with the last lanes above.
    await source.evaluate((el) => el.scrollIntoView({ block: "end" }));
    const from = await source.locator("span").first().boundingBox();
    const to = await laneLocator(page, "card:p76-coldline").boundingBox();
    await page.mouse.move(from.x + 10, from.y + 8);
    await page.mouse.down();
    await page.mouse.move(from.x + 40, from.y + 40, { steps: 4 });
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, {
      steps: 8,
    });
    await expect(page.locator("[data-swimlane-drag]")).toContainText(
      "Move under P76 coldline storage",
    );
    await page.mouse.up();
    await expect
      .poll(() =>
        api.posts.map((post) => [post.verb, post.ref, post.body.under]),
      )
      .toEqual([["move", "card:ramp-telemetry", "card:p76-coldline"]]);
    await expect(
      laneLocator(page, "card:p76-coldline").locator(
        '[data-work-ref="card:ramp-telemetry"]',
      ),
    ).toBeVisible();
  });

  test("a lost move receipt retries after reload with the original body key", async ({
    page,
    context,
  }) => {
    const api = seed();
    api.loseMoveResponses = 1;
    await open(page, context, api, { fixTime: false });
    await page
      .locator('[data-tray-ref="card:cost-4"] [data-group-under="card:margin"]')
      .click();
    await expect(
      page.locator('[data-mutation-state="retrying"]'),
    ).toBeVisible();
    await expect(
      page.getByText("Gateway timeout", { exact: true }),
    ).not.toBeVisible();
    // Another actor moves the card after the first send committed. Receipt
    // replay must confirm the original intent without applying it again.
    const original = api.posts[0];
    const parent = api.brief.initiatives.find(
      (entry) => entry.ref === "card:margin",
    );
    parent.rollup = rollup(
      parent.rollup.children.filter((entry) => entry.ref !== original.ref),
    );
    api.ungrouped.push({
      ref: original.ref,
      title: "Cost 4/5: Railway egress",
      needs_home: true,
      parent_ref: null,
    });
    await page.reload();
    await expect
      .poll(() => api.posts.filter((post) => post.verb === "move").length)
      .toBe(2);
    await expect(page.locator('[data-mutation-state="retrying"]')).toHaveCount(
      0,
    );
    expect(api.posts[1].body).toEqual(original.body);
    expect(original.key).toMatch(/^[0-9a-f-]{36}$/);
    expect(api.posts[1].headerKey).toBeUndefined();
    expect(api.moveEffects).toBe(1);
    await expect(page.locator('[data-tray-ref="card:cost-4"]')).toBeVisible();
  });

  for (const wanted of [true, false, "header-key"]) {
    test(`a saved legacy ${typeof wanted === "boolean" ? `top_level=${wanted}` : wanted} intent recovers as a keyed move`, async ({
      page,
      context,
    }) => {
      const api = seed();
      await open(page, context, api, { fixTime: false });
      await expect(
        page.locator('[data-promote="card:ramp-telemetry"]'),
      ).toBeVisible();
      await context.setOffline(true);
      const key = await page.evaluate(async (wanted) => {
        const { enqueueLocalMutation } =
          await import("/src/lib/mutationOutbox.js");
        const key = crypto.randomUUID();
        await enqueueLocalMutation({
          key,
          operation: wanted === "header-key" ? "work.move" : "work.top_level",
          target: "work:card:ramp-telemetry:grouping",
          draftTarget: "board:grouping",
          draft: {
            verb:
              wanted === "header-key"
                ? "move"
                : wanted
                  ? "promote"
                  : "unpromote",
            work: {
              ref: "card:ramp-telemetry",
              title: "Shared-speech ramp telemetry",
            },
            version: 3,
          },
          request: {
            ref: "card:ramp-telemetry",
            body:
              wanted === "header-key"
                ? { under: "card:capture" }
                : {
                    actor_id: "actor-comment",
                    if_version: 3,
                    patch: { top_level: wanted },
                  },
          },
          title: "Saved initiative change",
        });
        return key;
      }, wanted);
      await expect(
        page.locator('[data-mutation-state="pending"]'),
      ).toBeVisible();
      // Exercise the same persisted record through a fresh page runtime.
      await context.setOffline(false);
      await page.reload();
      await expect
        .poll(() => api.posts.filter((post) => post.verb === "move").length)
        .toBe(1);
      expect(api.posts[0]).toMatchObject({
        verb: "move",
        ref: "card:ramp-telemetry",
        key,
        body: {
          actor_id: "actor-comment",
          request_key: key,
          ...(wanted === "header-key"
            ? { under: "card:capture" }
            : {
                ...(wanted ? { root: true } : { unsorted: true }),
                reason: wanted
                  ? "Made an initiative from Watch"
                  : "Returned to Unsorted from Watch",
              }),
        },
      });
      expect(api.posts[0].headerKey).toBeUndefined();
      expect(api.posts.some((post) => post.verb === "patch")).toBe(false);
      expect(api.moveEffects).toBe(1);
    });
  }

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
      "Cards are waiting in Unsorted. Create an initiative to move them under.",
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
        .getByText("Unsorted cards didn’t load"),
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
    // Each lane is a card: one tag, the bar, and what waits on the reader.
    await expect(capture).toHaveAttribute("data-lane-collapsed", "true");
    await expect(capture.locator(".tag").first()).toHaveText("needs you");
    const line = capture.locator(
      '[data-lane-phone-lines] [data-work-ref="card:a1-funding"]',
    );
    await expect(line).toBeVisible();
    await expect(line.locator("xpath=..")).toContainText("you");
    // A quiet lane is its bar alone, with the cards a tap away.
    const retention = laneLocator(page, "card:retention");
    await expect(retention).toHaveAttribute("data-lane-collapsed", "true");
    await expect(retention.locator("[data-lane-phone-lines] a")).toHaveCount(0);
    // Opened, empty columns are not drawn on a phone.
    const margin = laneLocator(page, "card:margin");
    await margin.locator("[data-lane-phone-lines] button").tap();
    await expect(margin).toHaveAttribute("data-lane-collapsed", "false");
    await expect(margin.locator('[data-lane-column="moving"]')).toBeVisible();
    await expect(margin.locator('[data-lane-column="done"]')).toBeHidden();
    await expect(page.locator("[data-swimlane-tray]")).toBeHidden();
    await shot(page, "lanes-phone");
    await expectCleanLayout(page, "swimlane board phone");

    await page.locator("[data-swimlane-tray-open]").tap();
    const sheet = page.getByRole("dialog", { name: /Unsorted/ });
    await expect(sheet).toBeVisible();
    // The sheet's controls are its own: no id is used twice.
    expect(await duplicateIds(page)).toEqual([]);
    await shot(page, "lanes-phone-tray");
    await sheet.locator('[data-group-under="card:capture"]').tap();
    await expect
      .poll(() => api.posts.map((post) => [post.verb, post.ref]))
      .toEqual([["move", "card:a1-f4"]]);
    await sheet.getByRole("button", { name: "Done" }).tap();
    await expect(sheet).toHaveCount(0);
    await capture.locator("[data-lane-phone-lines] button").tap();
    await expect(capture.locator('[data-work-ref="card:a1-f4"]')).toBeVisible();
  });
});
