import { expect, test } from "@playwright/test";

const WS_HOME = "/o/local/w/local";

async function unlockShellWithActor(page, name) {
  await page.getByLabel("Display name").fill(name);
  await page.getByRole("button", { name: "Create and continue" }).click();
}

/**
 * Report a connected PM.
 *
 * The shell shows a PM surface only on a state core confirmed, and the PM
 * routes need an authenticated principal that the dev actor gate does not
 * establish — so against the e2e core `/pm/presence` answers 401 and the slot
 * stays a blank placeholder. A nav test should be about the nav, so it says
 * which state it is testing instead of inheriting the environment's.
 */
async function withConnectedPm(page) {
  await page.route("**/pm/presence", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        state: "connected",
        last_seen: new Date().toISOString(),
        runner: "Hermes",
        host: "studio",
        configured: true,
        connected: true,
      }),
    }),
  );
}

/** Report a PM that has never been onboarded, so the slot offers setup. */
async function withPmNotOnboarded(page) {
  await page.route("**/pm/presence", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ state: "not_onboarded" }),
    }),
  );
}

/** Tabs whose own label does not fit on one line at this viewport. */
async function crowdedBottomNavTabs(bottomNav) {
  return bottomNav.locator(".shell-bottom-nav-item").evaluateAll((els) =>
    els
      .map((el) => {
        const label = el.querySelector(":scope > .shell-bottom-nav-label");
        return {
          text: (label?.textContent || el.textContent || "")
            .replace(/\s+/g, " ")
            .trim(),
          itemOverflow: el.scrollWidth > el.clientWidth + 1,
          labelOverflow: label
            ? label.scrollWidth > label.clientWidth + 1
            : false,
          labelWrapped: label
            ? label.scrollHeight > label.clientHeight + 1 ||
              label.getClientRects().length > 1
            : false,
        };
      })
      .filter(
        (item) => item.itemOverflow || item.labelOverflow || item.labelWrapped,
      ),
  );
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

test("workspace root routes to Overview after activation without reloading", async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  const documents = [];
  page.on("request", (request) => {
    if (request.resourceType() === "document") documents.push(request.url());
  });

  await page.goto(`${WS_HOME}?dashboard=weekly#overview-reports`);
  await expect(page).toHaveURL(
    `${test.info().project.use.baseURL}${WS_HOME}?dashboard=weekly#overview-reports`,
  );
  await expect(page.getByLabel("Display name")).toBeVisible();
  const historyLength = await page.evaluate(() => history.length);
  await unlockShellWithActor(page, `Inbox User ${Date.now()}`);

  await expect(page).toHaveURL(
    `${test.info().project.use.baseURL}${WS_HOME}/overview?dashboard=weekly#overview-reports`,
  );
  await expect(
    page.getByRole("heading", { name: "Overview", exact: true }),
  ).toBeVisible();
  expect(documents).toHaveLength(1);
  expect(await page.evaluate(() => history.length)).toBe(historyLength);
});

test("mobile bottom navigation switches workspace routes", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await withConnectedPm(page);

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
  // surfaces, the PM slot and More. Workspace search is ⌘K plus a button in
  // each list header. The PM slot reads "Ask PM" because this test says a PM
  // is connected; with none onboarded it reads "PM setup", and until core
  // answers it holds its space with no link at all.
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
  expect(await crowdedBottomNavTabs(bottomNav)).toEqual([]);

  await page.getByRole("button", { name: "Search workspace" }).click();
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
});

test("bottom navigation keeps one line per tab while the PM is unconfigured", async ({
  page,
}) => {
  // The narrowest phone the shell targets, and the state the connected-PM
  // test above never reaches: the PM slot reads setup copy there, which is
  // the longest label the bar ever carries.
  await page.setViewportSize({ width: 320, height: 568 });
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await withPmNotOnboarded(page);

  await page.goto(`${WS_HOME}/docs`);
  await unlockShellWithActor(page, `Setup User ${Date.now()}`);

  const bottomNav = page.getByRole("navigation", {
    name: "Primary navigation",
  });
  const pmTab = bottomNav.locator('[data-pm-nav="setup"]');
  await expect(pmTab).toBeVisible();

  expect(await crowdedBottomNavTabs(bottomNav)).toEqual([]);
});

test("Docs reaches workspace search without the sidebar", async ({ page }) => {
  // Below 1024px the sidebar that holds ⌘K is hidden, so a phone can only
  // reach workspace search through a list header.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(`${WS_HOME}/docs`);
  await unlockShellWithActor(page, `Docs Search User ${Date.now()}`);

  await expect(
    page.getByRole("heading", { name: "Docs", exact: true }),
  ).toBeVisible();
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
