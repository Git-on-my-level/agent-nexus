import { expect, test } from "@playwright/test";

test("renders app shell with sidebar navigation", async ({ page }) => {
  await page.goto("/");

  await expect(
    page.getByRole("heading", { name: "Organization Autorunner UI" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Inbox" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Threads" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Artifacts" })).toBeVisible();
});

test("navigates to placeholder section", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Threads" }).click();

  await expect(page).toHaveURL(/\/threads$/);
  await expect(page.getByRole("heading", { name: "Threads" })).toBeVisible();
});
