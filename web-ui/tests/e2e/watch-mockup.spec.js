import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";
import { expectCleanLayout } from "../helpers/layoutAudit.js";
import { mockupBrief, mockupUngrouped } from "../fixtures/watchMockupSeed.js";

/**
 * Watch against the approved mockups (`Rollup`, `Board`, `BoardPhone`),
 * seeded with data shaped like theirs. Each test checks the elements the
 * mockup shows, at desktop and phone widths.
 *
 * `MOCKUP_SHOTS=<dir>` also writes the implementation screenshots used in
 * the PR's mockup-versus-implementation table.
 */
const WATCH = "/o/local/w/local/watch";
const NOW = "2026-10-24T12:00:00Z";

const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

async function setup(page, context) {
  await page.clock.setFixedTime(new Date(NOW));
  await context.addInitScript(() => {
    localStorage.setItem("anx_ui_actor_id:local", "actor-reader");
    localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await context.route(/\/actors(?:\?.*)?$/, (route) =>
    json(route, {
      actors: [
        { id: "actor-reader", display_name: "Reader", tags: ["human"] },
        { id: "actor-lead", display_name: "lead", tags: ["agent"] },
        { id: "actor-agents", display_name: "agents", tags: ["agent"] },
      ],
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
    (route) => json(route, mockupBrief(NOW)),
  );
  await context.route(
    (url) => url.pathname === "/work/ungrouped",
    (route) => json(route, { work: mockupUngrouped(NOW), has_more: false }),
  );
  await context.route(
    (url) => url.pathname === "/workspace/dashboard/reports",
    (route) => json(route, { status: "ok", reports: [], has_more: false }),
  );
}

async function shot(page, name) {
  if (!process.env.MOCKUP_SHOTS) return;
  // The shell scrolls inside <main>; grow the window to the content so the
  // shot shows the whole page, then put it back.
  const size = page.viewportSize();
  const height = await page.evaluate(() =>
    Math.max(
      ...[...document.querySelectorAll("main, main *")]
        .filter((el) => el.scrollHeight > el.clientHeight + 1)
        .map((el) => el.scrollHeight + el.getBoundingClientRect().top),
      document.documentElement.scrollHeight,
    ),
  );
  await page.setViewportSize({ width: size.width, height: Math.ceil(height) });
  await page.screenshot({
    path: `${process.env.MOCKUP_SHOTS}/${name}.png`,
    fullPage: true,
  });
  await page.setViewportSize(size);
}

for (const [name, viewport] of [
  ["desktop", { width: 1440, height: 900 }],
  ["phone", { width: 390, height: 844 }],
]) {
  test.describe(name, () => {
    test.use({ viewport, hasTouch: name === "phone" });

    test(`the list reads like the Rollup mockup (${name})`, async ({
      page,
      context,
    }) => {
      await setup(page, context);
      await page.goto(WATCH);
      const list = page.locator("[data-rollup-list]");
      await expect(list).toBeVisible();
      // Header: top-level initiatives and the cards rolled into them.
      await expect(page.locator("[data-watch-summary]")).toContainText(
        "7 initiatives · 29 cards rolled up into them",
      );
      // Chips: need you, slipping, moving; and how the list is sorted.
      await expect(list.locator("[data-collection-filter]")).toHaveText([
        "3 need you",
        "1 slipping",
        "1 moving",
      ]);
      await expect(list).toContainText(
        "Sorted by what needs you · children fold into their initiative",
      );
      // Rows in bucket order, done folded away, children never rows.
      expect(
        await list
          .locator("[data-collection-row]")
          .evaluateAll((rows) => rows.map((row) => row.dataset.collectionRow)),
      ).toEqual([
        "card:capture",
        "card:margin",
        "card:security",
        "card:retention",
        "card:smarter",
        "card:firmware",
      ]);
      const capture = list.locator('[data-collection-row="card:capture"]');
      await expect(capture.locator("[data-rollup-next]")).toContainText(
        "next: legacy queues gone, no silent loss",
      );
      await expect(capture.locator("[data-rollup-children]")).toContainText(
        "9 children in 3 workstreams",
      );
      await expect(
        list.locator(
          '[data-collection-row="card:smarter"] [data-rollup-promises]',
        ),
      ).toHaveText("on track");
      // Rows that need the reader open by default, with workstream kid rows.
      await expect(capture).toHaveAttribute("data-expanded", "true");
      await expect(capture.locator("[data-rollup-kid]")).toHaveCount(3);
      await expect(
        capture.locator('[data-rollup-kid="card:speaker-id"]'),
      ).toContainText("past promise");
      await expect(
        list.locator('[data-collection-row="card:smarter"]'),
      ).toHaveAttribute("data-expanded", "false");
      if (name === "desktop") {
        await expect(capture.locator("[data-rollup-promises]")).toHaveText(
          "2 of 9 past",
        );
        await expect(
          list.locator(
            '[data-collection-row="card:smarter"] [data-rollup-owner]',
          ),
        ).toHaveText("agents");
        await expect(
          list.locator(
            '[data-collection-row="card:margin"] [data-rollup-owner]',
          ),
        ).toHaveText("You");
      } else {
        // A phone drops Promises and Next, as the mockup does under 900px.
        await expect(capture.locator("[data-rollup-promises]")).toBeHidden();
      }
      await shot(page, `impl-rollup-${name}`);
      await expectCleanLayout(page, `watch rollup ${name}`);
      const axe = await new AxeBuilder({ page })
        .include("[data-rollup-list]")
        .analyze();
      expect(axe.violations).toEqual([]);
    });

    test(`the board reads like the Board mockup (${name})`, async ({
      page,
      context,
    }) => {
      await setup(page, context);
      await page.goto(`${WATCH}?view=lanes`);
      const board = page.locator("[data-swimlane-board]");
      const capture = board.locator('[data-lane-ref="card:capture"]');
      await expect(capture).toBeVisible();
      await expect(capture.locator("[data-lane-next]")).toContainText(
        "next: legacy queues gone, no silent loss",
      );
      if (name === "desktop") {
        await expect(board.locator("[data-swimlane-summary]")).toHaveText(
          "7 initiatives · 29 cards inside them · 1 loop on their own tab",
        );
        // Two asks and nothing else waits on the reader in Capture.
        await expect(capture.locator("[data-lane-needs]")).toHaveText(
          "2 need you",
        );
        await expect(capture.locator("[data-lane-slipping]")).toHaveText(
          "2 slipping",
        );
        const stuck = capture.locator('[data-lane-column="stuck"]');
        await expect(stuck.locator("[data-ask-ref]")).toHaveCount(2);
        await expect(
          stuck.locator('[data-ask-ref="inbox:ask-funding"]'),
        ).toContainText("ask · you");
        await expect(
          stuck.locator('[data-ask-ref="inbox:ask-funding"]'),
        ).toHaveAttribute("href", /\/decide\/ask-funding$/);
        // The child an ask covers is not drawn twice.
        await expect(
          stuck.locator('[data-work-ref="card:fallback-funding"]'),
        ).toHaveCount(0);
        await expect(
          capture.locator(
            '[data-lane-column="moving"] [data-work-ref="card:record-lane"]',
          ),
        ).toContainText("workstream");
        // An ask about a child stands in for that child in needs-you.
        await expect(
          board.locator(
            '[data-lane-ref="card:security"] [data-lane-column="stuck"] [data-ask-ref="inbox:ask-plugins"]',
          ),
        ).toBeVisible();
        // 260px initiative column, then the four state columns.
        const width = await capture
          .locator(":scope > div")
          .first()
          .evaluate((el) => el.getBoundingClientRect().width);
        expect(Math.round(width)).toBe(260);
        // The tray sits under the board, dashed, with one-tap homes.
        const tray = board.locator("[data-swimlane-tray]");
        await expect(tray).toContainText("Ungrouped · 3");
        await expect(
          tray.locator('[data-group-under="card:capture"]'),
        ).toHaveText(/Group under Every word captured/);
        await expect(tray).toContainText("likely: Unit economics");
        const below = await page.evaluate(() => {
          const lanes = document.querySelector(
            '[aria-label="Initiative lanes"]',
          );
          const tray = document.querySelector("[data-swimlane-tray]");
          return (
            tray.getBoundingClientRect().top >=
            lanes.getBoundingClientRect().bottom
          );
        });
        expect(below).toBe(true);
      } else {
        // Phone: cards with one tag, the bar, and the lines that matter.
        await expect(board.locator("[data-swimlane-summary]")).toHaveText(
          "7 initiatives · 5 decisions waiting",
        );
        await expect(capture).toHaveAttribute("data-lane-collapsed", "true");
        await expect(capture.locator(".tag").first()).toHaveText("needs you");
        const lines = capture.locator("[data-lane-phone-lines] li");
        await expect(lines.nth(0)).toContainText(
          "Fallback funding & autopay ceiling",
        );
        await expect(lines.nth(0)).toContainText("you");
        await expect(lines.last()).toContainText("+ 6 more");
        await expect(page.locator("[data-swimlane-tray-open]")).toContainText(
          "Ungrouped · 3",
        );
        await lines.last().locator("button").tap();
        await expect(capture).toHaveAttribute("data-lane-collapsed", "false");
        await expect(
          capture.locator('[data-lane-column="moving"]'),
        ).toBeVisible();
        await lines.last().locator("button").tap();
      }
      await shot(page, `impl-board-${name}`);
      await expectCleanLayout(page, `watch board ${name}`);
    });
  });
}
