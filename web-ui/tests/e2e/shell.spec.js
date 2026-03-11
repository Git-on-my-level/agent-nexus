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

  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Inbox", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Threads", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Artifacts", exact: true }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Threads", exact: true }).click();

  await expect(page).toHaveURL(/\/threads$/);
  await expect(page.getByRole("heading", { name: "Threads" })).toBeVisible();

  await page.getByRole("button", { name: "New thread" }).click();
  await page.getByLabel("Title").fill(threadTitle);
  await page.getByLabel("Summary").fill("Created from shell flow e2e test.");
  await page.getByRole("button", { name: "Create thread" }).click();

  await expect(page.getByRole("link", { name: threadTitle })).toBeVisible();
});

test("renders a dashboard on / and routes into inbox", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("oar_ui_actor_id", "actor-ops-ai");
  });

  await page.goto("/");

  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Thread health" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Recent artifacts" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Review Inbox" }).click();
  await expect(page).toHaveURL(/\/inbox$/);
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
});

test("shows partial-failure messaging when one dashboard source is unavailable", async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("oar_ui_actor_id", "actor-ops-ai");
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 503,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ error: "temporary outage" }),
    });
  });

  await page.goto("/");

  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await expect(
    page.getByText("Failed to load threads:", { exact: false }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Recent artifacts" }),
  ).toBeVisible();
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
