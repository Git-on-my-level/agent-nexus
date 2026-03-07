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
  const threadTitle = `E2E Thread ${Date.now()}`;

  await page.goto("/");

  await page.getByLabel("Display name").fill("E2E User");
  await page.getByRole("button", { name: "Create and continue" }).click();

  await expect(
    page.getByRole("heading", { name: "Organization Autorunner UI" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Inbox" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Threads" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Artifacts" })).toBeVisible();

  await page.getByRole("link", { name: "Threads" }).click();

  await expect(page).toHaveURL(/\/threads$/);
  await expect(page.getByRole("heading", { name: "Threads" })).toBeVisible();

  await page.getByRole("button", { name: "New thread" }).click();
  await page.getByLabel("Title").fill(threadTitle);
  await page.getByLabel("Summary").fill("Created from shell flow e2e test.");
  await page.getByRole("button", { name: "Create thread" }).click();

  await expect(page.getByRole("link", { name: threadTitle })).toBeVisible();
});

test("opens mobile drawer navigation and navigates between routes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() => {
    window.localStorage.setItem("oar_ui_actor_id", "actor-ops-ai");
  });

  await page.goto("/inbox");

  const drawer = page.getByRole("dialog", { name: "Navigation menu" });
  await expect(drawer).toHaveCount(0);

  await page.getByRole("button", { name: "Open navigation menu" }).click();
  await expect(drawer).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(drawer).toHaveCount(0);

  await page.getByRole("button", { name: "Open navigation menu" }).click();
  await drawer
    .getByRole("link", { name: "Artifacts", exact: true })
    .click({ force: true });

  await expect(page).toHaveURL(/\/artifacts$/);
  await expect(page.getByRole("heading", { name: "Artifacts" })).toBeVisible();
});
