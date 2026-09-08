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

test("registers actor, unlocks shell, and opens Inbox", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("E2E User");
  await page.getByRole("button", { name: "Create and continue" }).click();

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/inbox/);
  await expect(
    page.getByRole("heading", { name: "Inbox", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Tasks", exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Docs", exact: true }).first(),
  ).toBeVisible();
});

test("workspace root routes to Inbox", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(WS_HOME);
  await unlockShellWithActor(page, `Inbox User ${Date.now()}`);

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/inbox/);
  await expect(
    page.getByRole("heading", { name: "Inbox", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("navigation", { name: "Inbox mailbox" }),
  ).toBeVisible();
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
  await bottomNav.getByRole("link", { name: "Tasks" }).click();

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/tasks/);
  await expect(
    page.getByRole("heading", { name: "Tasks", exact: true }),
  ).toBeVisible();

  await bottomNav.getByRole("button", { name: "Search workspace" }).click();
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
});
