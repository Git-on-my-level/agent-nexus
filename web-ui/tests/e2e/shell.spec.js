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

test("registers actor, unlocks shell, and opens Overview", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("E2E User");
  await page.getByRole("button", { name: "Create and continue" }).click();

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/overview/);
  await expect(
    page.getByRole("heading", { name: "Overview", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Tasks", exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Docs", exact: true }).first(),
  ).toBeVisible();
});

test("workspace root routes to Overview", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(WS_HOME);
  await unlockShellWithActor(page, `Inbox User ${Date.now()}`);

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/overview/);
  await expect(
    page.getByRole("heading", { name: "Overview", exact: true }),
  ).toBeVisible();
});

test("mobile bottom navigation switches workspace routes", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
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

  // Search left the bottom bar: the bar carries Overview, the product
  // surfaces, Ask PM and More. Workspace search is ⌘K plus a button in
  // each list header.
  await expect(
    bottomNav.getByRole("button", { name: "Search workspace" }),
  ).toHaveCount(0);
  for (const name of [
    "Overview",
    "Inbox",
    "Agents",
    "Tasks",
    "Docs",
    "Ask PM",
    "More",
  ]) {
    await expect(bottomNav.getByRole("link", { name })).toBeVisible();
  }
  const crowded = await bottomNav
    .locator(".shell-bottom-nav-item")
    .evaluateAll((els) =>
      els
        .map((el) => {
          const label = el.querySelector(":scope > span:last-of-type");
          return {
            text: (label?.textContent || el.textContent || "")
              .replace(/\s+/g, " ")
              .trim(),
            itemOverflow: el.scrollWidth > el.clientWidth + 1,
            labelWrapped: label
              ? label.scrollHeight > label.clientHeight + 1 ||
                label.getClientRects().length > 1
              : false,
          };
        })
        .filter((item) => item.itemOverflow || item.labelWrapped),
    );
  expect(crowded).toEqual([]);

  await page.getByRole("button", { name: "Search workspace" }).click();
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
});

test("sidebar account menu reaches settings and sign out", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(`${WS_HOME}/inbox`);
  await unlockShellWithActor(page, `Menu User ${Date.now()}`);

  const sidebar = page.getByRole("complementary", { name: "Primary" });
  // The footer is one account row; every settings destination and the identity
  // action live in the menu it opens, not in a permanent block.
  await expect(sidebar.getByRole("link", { name: "Access" })).toHaveCount(0);

  await sidebar.getByRole("button", { expanded: false }).last().click();

  const menu = page.getByRole("menu");
  for (const name of ["Access", "Secrets", "Integrations", "Audit"]) {
    await expect(menu.getByRole("menuitem", { name })).toBeVisible();
  }
  await expect(
    menu.getByRole("button", { name: /Sign out|Switch identity/ }),
  ).toBeVisible();
});
