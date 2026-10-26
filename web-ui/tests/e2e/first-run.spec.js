/**
 * The first-run path: a new signup lands in a workspace nothing has used yet.
 *
 * Captures the surfaces that first visit goes through (Initiatives, Decide,
 * Watch and Agents) and asserts each reads as empty rather than broken. The
 * connect-your-first-agent panel is unit-covered (`firstAgentPanel.test.js`).
 */
import { mkdir } from "node:fs/promises";

import { expect, test } from "@playwright/test";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";
import { waitForAppReady } from "../helpers/pageReady.js";
import brief from "../../../contracts/fixtures/brief/brief.v1.json" with { type: "json" };

const BEFORE = process.env.FIRST_RUN_CAPTURE_BEFORE === "1";
const SHOTS =
  process.env.FIRST_RUN_SHOTS ||
  `.screenshots/review/first-run${BEFORE ? "-before" : ""}`;

const WS = "/o/local/w/local";

/*
 * Every test here opens a workspace route the dev server has not compiled
 * yet. The budget below covers the compile, not the behaviour.
 */
const FIRST_PAINT = { timeout: 30_000 };

// eslint-disable-next-line no-empty-pattern
test.beforeEach(async ({}, testInfo) => {
  testInfo.setTimeout(100_000);
});

/**
 * A workspace that was provisioned and never used.
 *
 * @param {import("@playwright/test").Page} page
 */
async function installEmptyWorkspace(page) {
  await installWorkspaceApi(page, {
    actors: [{ id: "operator", display_name: "Alex", tags: ["human"] }],
    principals: [{ actor_id: "operator", principal_kind: "human" }],
    agents: [],
    hosts: [],
    work: [],
    // A workspace with no machine in it has no PM either.
    pm: {
      state: "not_configured",
      configured: false,
      connected: false,
      last_seen: null,
    },
  });
  await page.route(
    (url) => url.pathname === "/brief",
    (route) =>
      route.fulfill({
        json: {
          ...brief,
          generated_at: new Date().toISOString(),
          decisions: [],
          initiatives: [],
          loops: [],
          daily_digest: [],
          your_asks: [],
          answered_for_you: [],
        },
      }),
  );
}

test("the first-run path reads as empty, not broken", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await mkdir(SHOTS, { recursive: true });
  await installEmptyWorkspace(page);

  await page.goto(`${WS}/initiatives`);
  await waitForAppReady(page);
  await expect(page.getByText("Nothing tracked yet")).toBeVisible(FIRST_PAINT);
  await page.screenshot({ path: `${SHOTS}/2-initiatives.png`, fullPage: true });

  await page.goto(`${WS}/decide`);
  await waitForAppReady(page);
  await expect(page.locator("[data-decide-empty]")).toBeVisible(FIRST_PAINT);
  await page.screenshot({ path: `${SHOTS}/3-decide.png`, fullPage: true });

  await page.goto(`${WS}/watch`);
  await waitForAppReady(page);
  await expect(page.locator("[data-watch-empty]")).toBeVisible(FIRST_PAINT);
  await expect(page.getByRole("region", { name: "Daily digest" })).toHaveCount(
    0,
  );
  await page.screenshot({ path: `${SHOTS}/4-watch.png`, fullPage: true });

  await page.goto(`${WS}/agents`);
  await waitForAppReady(page);
  await expect(page.locator("[data-agents-empty]")).toBeVisible(FIRST_PAINT);
  await page.screenshot({ path: `${SHOTS}/5-agents.png`, fullPage: true });
});
