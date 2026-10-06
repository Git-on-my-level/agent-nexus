import { mkdir } from "node:fs/promises";

import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * The Overview initiative tiles and the initiative page, in a real browser.
 *
 * Only one theme is captured because only one exists: `app.css` defines a
 * single dark token set, with no `data-theme` and no `prefers-color-scheme`
 * block. `chart-interaction.spec.js` covers the one thing a light theme would
 * change here — that chart series colours are chosen from the background
 * rather than baked in.
 */

const WORKSPACE = "/o/local/w/local";
const CARD_REF = "card:release-b";
const CARD_PATH = `${WORKSPACE}/tasks/${encodeURIComponent(CARD_REF)}`;
const NOW = "2026-10-04T12:00:00Z";

const plan = {
  steps: [
    {
      id: "contracts",
      title: "Shared report contracts",
      after: [],
      ref: "card:contracts",
    },
    { id: "plan-model", title: "Plan model on cards", after: ["contracts"] },
    { id: "ui-chips", title: "Ref chips everywhere", after: ["contracts"] },
    { id: "tiles", title: "Overview tiles", after: ["plan-model", "ui-chips"] },
  ],
};

const planState = {
  steps: [
    { id: "contracts", status: "done", resolvable: true },
    { id: "plan-model", status: "active", resolvable: false },
    { id: "ui-chips", status: "blocked", resolvable: false },
    { id: "tiles", status: "not_started", resolvable: false },
  ],
  progress: { done: 1, total: 4 },
  critical_path: ["plan-model", "tiles"],
  next_steps: ["plan-model"],
  shape: "dag",
  health: "blocked",
  last_movement_at: "2026-10-04T09:00:00Z",
};

/** A live initiatives projection row, as core sends it. */
const initiative = (overrides = {}) => ({
  ref: CARD_REF,
  title: "Release B",
  summary: "Initiative plans, live dashboards and agent ergonomics.",
  priority: "p1",
  phase: "in_progress",
  progress: { done: 1, total: 4 },
  needs: [],
  board_ref: "board:release-b",
  updated_at: "2026-10-04T09:00:00Z",
  ...overrides,
});

/** `geometry` as core serializes it: layer and `after` per node, bounded. */
const geometry = (shape, nodes, collapsed = 0) => ({
  shape,
  nodes,
  total_nodes: nodes.length + collapsed,
  collapsed_nodes: collapsed,
});

async function installInitiativePage(page) {
  await page.clock.setFixedTime(new Date(NOW));
  await installWorkspaceApi(page, {});
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (!["fetch", "xhr"].includes(request.resourceType())) {
      return route.fallback();
    }
    const url = new URL(request.url());
    const path = decodeURIComponent(url.pathname);
    const json = (body) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (path.endsWith("/plan") && request.method() === "GET") {
      return json({ plan, plan_state: planState });
    }
    if (path === "/refs/resolve" && request.method() === "POST") {
      return json({
        items: [
          {
            ref: "card:contracts",
            kind: "card",
            title: "Shared report contracts",
            status: "done",
            owner: "Codex Luna",
            progress: { done: 5, total: 5 },
            resolvable: true,
          },
        ],
      });
    }
    if (path.startsWith("/work/") && request.method() === "GET") {
      return json({
        work: {
          ref: CARD_REF,
          handle: "release-b",
          title: "Release B",
          summary:
            "Initiative plans, live dashboards and agent ergonomics.\n\nThe card body sits below the plan.",
          phase: "in_progress",
          priority: "high",
          definition_of_done: [
            "Plans render in the shape the plan has",
            "Tiles answer state and progress in ten seconds",
          ],
          next_action: "Wire the Overview tiles",
          source: { authority: "nexus" },
        },
      });
    }
    return route.fallback();
  });
}

test("initiative page leads with the plan, and the card body follows", async ({
  page,
}, testInfo) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.goto(CARD_PATH);

  const planSection = page.locator("[data-initiative-plan]");
  await expect(planSection).toBeVisible({ timeout: 60_000 });

  // Status line and health lead the page. Scoped to the health badge, since
  // "Blocked" also labels the blocked steps inside the diagram. The page has
  // room for words, so the badge is the pill form: a glyph and the label.
  const planHealth = planSection.locator("[data-health]").first();
  await expect(planHealth).toHaveAttribute("data-health", "blocked");
  await expect(planHealth).toContainText("Blocked");
  await expect(page.locator("[data-plan-progress]")).toHaveText("1/4 steps");

  // The plan's shape picks the view: this one branches, so it is a tree.
  await expect(page.locator("[data-plan-shape='dag']")).toBeVisible();
  await expect(page.locator(".plan-node")).toHaveCount(4);
  await expect(page.locator(".plan-edge--critical")).toHaveCount(1);

  // A step with a ref renders as a resolved chip, not a bare id.
  await expect(
    page.locator(".plan-node [data-anx-ref='card:contracts']"),
  ).toContainText("Shared report contracts");

  // The card body is present, and below the plan.
  const order = await page
    .locator("[data-initiative-plan], [data-initiative-body]")
    .evaluateAll((nodes) =>
      nodes.map((node) =>
        node.hasAttribute("data-initiative-plan") ? "plan" : "body",
      ),
    );
  expect(order).toEqual(["plan", "body"]);

  await mkdir(".screenshots/review", { recursive: true });
  await page.screenshot({
    path: ".screenshots/review/initiative-desktop.png",
    animations: "disabled",
  });
  await page.screenshot({
    path: testInfo.outputPath("initiative-desktop.png"),
    animations: "disabled",
  });
  await testInfo.attach("initiative-desktop", {
    path: testInfo.outputPath("initiative-desktop.png"),
    contentType: "image/png",
  });
});

test("the plan diagram keeps a readable fallback and a focusable chip", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });

  // Edges are decoration; the dependencies they draw are also written out.
  await expect(page.locator(".plan-tree__edges")).toHaveAttribute(
    "aria-hidden",
    "true",
  );
  await page.getByText("View plan steps").click();
  const steps = page.locator(".plan-steps li");
  await expect(steps).toHaveCount(4);
  await expect(
    steps.filter({ hasText: "after Shared report contracts" }),
  ).not.toHaveCount(0);

  // A chip inside the diagram is a real link the keyboard can reach.
  const chip = page.locator(".plan-node a[data-anx-ref='card:contracts']");
  await chip.focus();
  await expect(chip).toBeFocused();
  await expect(page.locator(".anx-ref-preview")).toBeVisible();
  await expect(page.locator(".anx-ref-preview")).toContainText("Codex Luna");
});

test("initiative page has no accessibility violations", async ({ page }) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });
  const results = await new AxeBuilder({ page })
    .include("[data-initiative-plan]")
    .withTags(["wcag2a", "wcag2aa"])
    .analyze();
  expect(results.violations).toEqual([]);
});

test("the plan reads at phone width", async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });

  // The tree scrolls sideways rather than squashing or overflowing the page.
  const scroller = page.locator(".plan-tree-scroll");
  await expect(scroller).toBeVisible();
  const overflows = await scroller.evaluate(
    (node) => node.scrollWidth > node.clientWidth,
  );
  expect(overflows).toBe(true);
  const documentOverflows = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth + 1,
  );
  expect(documentOverflows).toBe(false);

  await mkdir(".screenshots/review", { recursive: true });
  await page.screenshot({
    path: ".screenshots/review/initiative-phone.png",
    animations: "disabled",
  });
  await page.screenshot({
    path: testInfo.outputPath("initiative-phone.png"),
    animations: "disabled",
  });
  await testInfo.attach("initiative-phone", {
    path: testInfo.outputPath("initiative-phone.png"),
    contentType: "image/png",
  });
});

/**
 * The Overview tiles with real plan state behind them: health, a mini-viz sized
 * to the plan's shape, what is next, and when it last moved. The seeded fixture
 * in overview.spec.js predates plans, so a tile there degrades to progress
 * alone; this one shows the whole tile.
 */
const OVERVIEW = `${WORKSPACE}/overview`;

async function installOverviewTiles(page) {
  await page.clock.setFixedTime(new Date(NOW));
  await installWorkspaceApi(page, {});
  const snapshot = {
    generated_at: NOW,
    needs_you: {
      status: "ok",
      count: 2,
      rows: [],
      href: "/inbox?mailbox=needs-you",
    },
    initiatives: {
      status: "ok",
      count: 3,
      items: [
        initiative({
          plan_state: planState,
          health: {
            status: "blocked",
            reason: "A step on the critical path is blocked.",
          },
          geometry: geometry("dag", [
            { id: "contracts", status: "done", layer: 0, after: [] },
            {
              id: "plan-model",
              status: "active",
              layer: 1,
              after: ["contracts"],
            },
            {
              id: "ui-chips",
              status: "blocked",
              layer: 1,
              after: ["contracts"],
            },
            {
              id: "tiles",
              status: "not_started",
              layer: 2,
              after: ["plan-model", "ui-chips"],
            },
          ]),
          needs: ["Ref chips everywhere"],
        }),
        initiative({
          ref: "card:dashboards",
          title: "Live dashboards",
          summary: "Panels that never go stale.",
          progress: { done: 4, total: 5 },
          plan_state: {
            ...planState,
            steps: [
              { id: "contracts", status: "done" },
              { id: "panels", status: "done" },
              { id: "queries", status: "done" },
              { id: "publish", status: "done" },
              { id: "adopt", status: "active" },
            ],
            progress: { done: 4, total: 5 },
            critical_path: ["adopt"],
            next_steps: ["adopt-the-dashboard"],
            shape: "chain",
            health: "on_track",
            last_movement_at: "2026-10-04T11:30:00Z",
          },
          health: { status: "on_track", reason: "Work is progressing." },
          geometry: geometry(
            "chain",
            [
              { id: "contracts", status: "done", layer: 0, after: [] },
              { id: "panels", status: "done", layer: 1, after: ["contracts"] },
              { id: "queries", status: "done", layer: 2, after: ["panels"] },
              { id: "publish", status: "done", layer: 3, after: ["queries"] },
              { id: "adopt", status: "active", layer: 4, after: ["publish"] },
            ],
            0,
          ),
        }),
        initiative({
          ref: "card:ergonomics",
          title: "Agent ergonomics",
          summary: "Fewer traps in the CLI and the skills.",
          progress: { done: 1, total: 6 },
          plan_state: {
            ...planState,
            steps: [
              { id: "audit", status: "done" },
              { id: "cli", status: "not_started" },
              { id: "skills", status: "not_started" },
            ],
            progress: { done: 1, total: 3 },
            critical_path: ["cli"],
            next_steps: ["cli-first-class-inbox"],
            shape: "lanes",
            health: "stalled",
            last_movement_at: "2026-09-26T09:00:00Z",
          },
          health: {
            status: "stalled",
            reason: "Nothing has moved for 8 days.",
          },
          geometry: geometry("lanes", [
            { id: "audit", status: "done", layer: 0, after: [] },
            { id: "cli", status: "not_started", layer: 0, after: [] },
            { id: "skills", status: "not_started", layer: 1, after: ["cli"] },
          ]),
        }),
      ],
    },
    since_you_last_looked: {
      since: "2026-10-03T12:00:00Z",
      generated_at: NOW,
      items: [
        {
          kind: "initiative_blocked",
          ref: "card:release-b",
          title: "Release B",
        },
        {
          kind: "step_completed",
          ref: "card:dashboards",
          title: "Publish",
          step_id: "publish",
        },
        {
          kind: "ask_answered",
          ref: "event:launch",
          title: "Launch date",
          ts: "2026-10-04T11:00:00Z",
        },
      ],
      truncated: false,
    },
    dashboard: { status: "ok", pinned_ref: "", has_more: false, reports: [] },
    agents: { status: "ok", rows: [], count: 0 },
    work: { status: "ok", total: 3, human_count: 0, items: [] },
  };
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (!["fetch", "xhr"].includes(request.resourceType())) {
      return route.fallback();
    }
    const path = decodeURIComponent(new URL(request.url()).pathname);
    if (path === "/overview" || path === "/workspace/dashboard") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(snapshot),
      });
    }
    return route.fallback();
  });
}

for (const viewport of [
  { width: 1440, height: 1000, label: "desktop" },
  { width: 390, height: 844, label: "phone" },
]) {
  test(`Overview initiative tiles @ ${viewport.label}`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(90_000);
    await installOverviewTiles(page);
    await page.setViewportSize({
      width: viewport.width,
      height: viewport.height,
    });
    await page.goto(OVERVIEW);

    const tiles = page.locator("[data-initiative-tile]");
    await expect(tiles.first()).toBeVisible({ timeout: 60_000 });
    await expect(tiles).toHaveCount(3);

    /*
     * Worst first. The sort is the dashboard's whole job: blocked, then stale,
     * then on track — whatever order the projection sent them in.
     */
    await expect(tiles.nth(0)).toHaveAttribute("data-tile-health", "blocked");
    await expect(tiles.nth(1)).toHaveAttribute("data-tile-health", "stale");
    await expect(tiles.nth(2)).toHaveAttribute("data-tile-health", "on_track");

    // Each tile answers state and progress without opening anything.
    await expect(tiles.first()).toContainText("Release B");
    await expect(tiles.first()).toContainText("Tech tree");
    await expect(tiles.first()).toContainText("Next: Plan model");
    await expect(tiles.first().locator("[data-tile-needs]")).toContainText(
      "Ref chips everywhere",
    );

    // Health is a compact badge: a glyph, with the full text and core's
    // reason on hover rather than truncated to "Blo…" in a tile header.
    const health = tiles.first().locator("[data-health]");
    await expect(health).toHaveAttribute("data-health", "blocked");
    await expect(health).toHaveAttribute(
      "title",
      "Blocked — A step on the critical path is blocked.",
    );

    // The age is a badge: "3h", with the verb and exact instant on hover.
    const age = tiles.first().locator("time.age-badge");
    await expect(age).toHaveText("3h");
    await expect(age).toHaveAttribute("title", /^Moved .*\(3h\)$/);

    await expect(tiles.nth(1)).toContainText("Agent ergonomics");
    await expect(tiles.nth(1)).toContainText("Lanes");
    await expect(tiles.nth(2)).toContainText("Live dashboards");
    await expect(tiles.nth(2)).toContainText("Timeline");

    /*
     * One flat bar per tile — a segment per step, in plan order. The tile used
     * to draw the plan's real shape in miniature; at tile size that became a
     * block of colour tall enough to crowd the rows under it, and said less
     * than the shape's own name does in the line below. The shape is still
     * named, and the graph itself is a click away on the initiative page.
     */
    for (const index of [0, 1, 2]) {
      const bar = tiles.nth(index).locator(".tile-bar");
      await expect(bar).toHaveCount(1);
      // A single row: every segment shares one offsetTop.
      const rows = await bar.evaluate(
        (node) =>
          new Set(
            [...node.querySelectorAll(".seg")].map((seg) => seg.offsetTop),
          ).size,
      );
      expect(rows).toBe(1);
      // And it is 8px tall, the height the mockup draws it at.
      const height = await bar
        .locator(".seg")
        .first()
        .evaluate((node) => Math.round(node.getBoundingClientRect().height));
      expect(height).toBe(8);
    }
    // Release B's plan has four steps, so the bar has four segments.
    await expect(tiles.first().locator(".tile-bar .seg")).toHaveCount(4);

    // What changed since this viewer last looked, from the server digest.
    const strip = page.locator("[data-since-you-last-looked]");
    await expect(strip).toBeVisible();
    await expect(strip).toContainText("Since you last looked");
    await expect(strip).toContainText(
      "1 step done · 1 blocked · 1 ask answered",
    );
    await expect(strip.locator("[data-since-kind]").first()).toHaveAttribute(
      "data-since-kind",
      "initiative_blocked",
    );

    // The whole tile is the link to the initiative page.
    await expect(tiles.first()).toHaveAttribute(
      "href",
      new RegExp("/tasks/card%3Arelease-b$"),
    );

    // One urgent band, not a restatement of the Inbox: it counts what is
    // waiting, and names the initiative that has stopped moving.
    const band = page.locator('[data-overview-section="urgent"]');
    await expect(band).toHaveAttribute("data-urgent-state", "active");
    await expect(band.locator("[data-urgent-ask-count]")).toHaveText("2 asks");
    await expect(
      band.locator("[data-urgent-initiative='card:release-b']"),
    ).toContainText("Release B");

    // And it is above the initiatives, which are above the dashboard.
    const order = await page
      .locator("[data-overview-section]")
      .evaluateAll((nodes) =>
        nodes.map((node) => node.dataset.overviewSection),
      );
    expect(order.slice(0, 3)).toEqual(["urgent", "initiatives", "reports"]);

    const section = page.locator('[data-overview-section="initiatives"]');
    await section.screenshot({
      path: testInfo.outputPath(`tiles-${viewport.label}.png`),
      animations: "disabled",
    });
    await testInfo.attach(`tiles-${viewport.label}`, {
      path: testInfo.outputPath(`tiles-${viewport.label}.png`),
      contentType: "image/png",
    });

    // The whole page, so the digest strip and the single Inbox line are in
    // frame alongside the tiles.
    await mkdir(".screenshots/review", { recursive: true });
    await page.screenshot({
      path: `.screenshots/review/overview-${viewport.label}.png`,
      animations: "disabled",
    });
    await page.screenshot({
      path: testInfo.outputPath(`overview-${viewport.label}.png`),
      animations: "disabled",
    });
    await testInfo.attach(`overview-${viewport.label}`, {
      path: testInfo.outputPath(`overview-${viewport.label}.png`),
      contentType: "image/png",
    });
  });
}

test("Overview tiles have no accessibility violations", async ({ page }) => {
  test.setTimeout(90_000);
  await installOverviewTiles(page);
  await page.goto(OVERVIEW);
  await expect(page.locator("[data-initiative-tile]").first()).toBeVisible({
    timeout: 60_000,
  });
  const results = await new AxeBuilder({ page })
    .include('[data-overview-section="initiatives"]')
    .withTags(["wcag2a", "wcag2aa"])
    .analyze();
  expect(results.violations).toEqual([]);
});

/**
 * The preview's keyboard path, against the real chip — the unit tests use a
 * stand-in anchor, which does not carry the chip's own focus handler and so
 * cannot show either of these failures.
 */
test("the preview is reachable by Tab and Escape does not reopen it", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installInitiativePage(page);
  await page.goto(CARD_PATH);
  await expect(page.locator("[data-initiative-plan]")).toBeVisible({
    timeout: 60_000,
  });

  const chip = page.locator(".plan-node a[data-anx-ref='card:contracts']");
  const card = page.locator(".anx-ref-preview");

  await chip.focus();
  await expect(card).toBeVisible();

  // Tab steps into the card's own controls, not past them to the next page
  // control. The card is appended at the end of the document, so document
  // order alone would never get a reader here.
  await page.keyboard.press("Tab");
  await expect(card).toBeVisible();
  const inCard = await card.evaluate((node) =>
    node.contains(document.activeElement),
  );
  expect(inCard).toBe(true);

  // Walk to Copy ref and dismiss from there.
  const copy = card.locator("button", { hasText: "Copy ref" });
  await copy.focus();
  await expect(copy).toBeFocused();
  await page.keyboard.press("Escape");

  // Escape restores focus to the chip, and that focus must not reopen the
  // dialog the reader just dismissed.
  await expect(card).toBeHidden();
  await expect(chip).toBeFocused();
  await page.waitForTimeout(250);
  await expect(card).toBeHidden();
});
