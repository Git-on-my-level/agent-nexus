import { expect, test } from "@playwright/test";

const WS_HOME = "/o/local/w/local";

async function unlockShellWithActor(page, name) {
  await page.getByLabel("Display name").fill(name);
  await page.getByRole("button", { name: "Create and continue" }).click();
}

test("blocks shell with actor gate when no actor is selected", async ({
  page,
}) => {
  await page.goto(WS_HOME);

  await expect(
    page.getByRole("heading", { name: "Select Actor Identity" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Inbox" })).toHaveCount(0);
});

test("registers actor, unlocks shell, and performs a write", async ({
  page,
}) => {
  const threadTitle = `E2E Thread ${Date.now()}`;

  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(WS_HOME);

  await page.getByLabel("Display name").fill("E2E User");
  await page.getByRole("button", { name: "Create and continue" }).click();

  await expect(page.getByRole("heading", { name: "Home" })).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Inbox", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Topics", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Artifacts", exact: true }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Topics", exact: true }).click();

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/topics$/);
  await expect(
    page.getByRole("heading", { name: "Topics", exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "New topic" }).click();
  await page.getByLabel("Title").fill(threadTitle);
  await page.getByLabel("Summary").fill("Created from shell flow e2e test.");
  await page.getByRole("button", { name: "Create topic" }).click();

  await expect(page.getByRole("link", { name: threadTitle })).toBeVisible();
});

test("renders Home on workspace root and routes into inbox", async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(WS_HOME);
  await unlockShellWithActor(page, `Home User ${Date.now()}`);

  await expect(page.getByRole("heading", { name: "Home" })).toBeVisible();
  await expect(
    page.locator(
      '[data-testid="home-unread-feed"], [data-testid="home-unread-empty"], [data-testid="home-unread-loading"]',
    ),
  ).toHaveCount(1);

  await page.getByRole("link", { name: "Inbox", exact: true }).first().click();
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/inbox$/);
  await expect(
    page.getByRole("heading", { name: "Inbox", exact: true }),
  ).toBeVisible();
});

test("shows error when Home unread feed is unavailable", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.route(/\/home\/unread(\?.*)?$/, async (route) => {
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

  await page.goto(WS_HOME);
  await unlockShellWithActor(page, `Outage User ${Date.now()}`);

  await expect(page.getByRole("heading", { name: "Home" })).toBeVisible();
  await expect(page.getByRole("alert")).toBeVisible();
});

test("mobile bottom navigation switches workspace routes", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(`${WS_HOME}/inbox`);
  await unlockShellWithActor(page, `Mobile User ${Date.now()}`);

  const bottomNav = page.getByRole("navigation", {
    name: "Primary navigation",
  });
  await bottomNav.getByRole("link", { name: "Topics" }).click();

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/topics$/);
  await expect(
    page.getByRole("heading", { name: "Topics", exact: true }),
  ).toBeVisible();

  await bottomNav.getByRole("button", { name: "Search workspace" }).click();
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
});
