import { expect, test } from "@playwright/test";

test("blocks shell with actor gate when no actor is selected", async ({
  page,
}) => {
  await page.goto("/");

  await expect(
    page.getByRole("heading", { name: "Select Actor Identity" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Inbox" })).toHaveCount(0);
});

test("registers actor, unlocks shell, and performs a write", async ({
  page,
}) => {
  await page.goto("/");

  await page.getByLabel("Display name").fill("E2E User");
  await page.getByRole("button", { name: "Create and continue" }).click();

  await expect(
    page.getByRole("heading", { name: "Organization Autorunner UI" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Inbox" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Threads" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Artifacts" })).toBeVisible();

  await page.getByRole("button", { name: "Post Sample Message" }).click();
  await expect(
    page.getByText("created_by: E2E User", { exact: false }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Threads" }).click();

  await expect(page).toHaveURL(/\/threads$/);
  await expect(page.getByRole("heading", { name: "Threads" })).toBeVisible();
});
