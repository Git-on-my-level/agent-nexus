import { expect, test } from "@playwright/test";

/**
 * A touch on a timestamp inside a link must show the exact local time and
 * must not follow the link. A later tap on the rest of the link still
 * navigates, and a tap outside the timestamp dismisses the tip.
 *
 * jsdom does not synthesize the click Chromium sends after a cancelled
 * pointerdown, so this has to run in a touch-enabled browser.
 */

test.use({ hasTouch: true });

test("a touch on a timestamp inside a link does not navigate", async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("anx_ui_actor_id:local", "actor-time-touch");
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: "actor-time-touch", display_name: "Time Touch" }],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() !== "GET" || request.resourceType() === "document") {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        threads: [
          {
            id: "thread-onboarding",
            title: "Customer Onboarding Workflow",
            status: "active",
            updated_at: "2026-03-03T11:00:00.000Z",
          },
        ],
      }),
    });
  });

  await page.goto("/o/local/w/local/threads");
  const link = page.getByRole("link", {
    name: /Customer Onboarding Workflow/,
  });
  const time = link.locator("time");
  await expect(time).toBeVisible();

  await time.tap();
  await expect(page.locator(".anx-tooltip")).toBeVisible();
  await expect(page).toHaveURL(/\/threads$/);

  await page.keyboard.press("Escape");
  await expect(page.locator(".anx-tooltip")).toHaveCount(0);

  await time.tap();
  await expect(page.locator(".anx-tooltip")).toBeVisible();
  await page.getByRole("button", { name: "Search the workspace" }).tap();
  await expect(page.locator(".anx-tooltip")).toHaveCount(0);
  await expect(page).toHaveURL(/\/threads$/);
  // Search opens the command palette. Close it, then tap the title — the
  // part of the link that is not the timestamp — and the row navigates.
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toHaveCount(0);

  await link.locator("p").first().tap();
  await expect(page).toHaveURL(/\/threads\/thread-onboarding/);
});
